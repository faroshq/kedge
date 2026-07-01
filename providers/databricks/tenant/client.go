// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	databricksv1alpha1 "github.com/faroshq/provider-databricks/apis/databricks/v1alpha1"
	"github.com/faroshq/provider-databricks/queryapi"
)

var (
	tablesGVR      = databricksv1alpha1.SchemeGroupVersion.WithResource("tables")
	warehousesGVR  = databricksv1alpha1.SchemeGroupVersion.WithResource("warehouses")
	connectionsGVR = databricksv1alpha1.SchemeGroupVersion.WithResource("connections")
	secretsGVR     = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
)

const (
	defaultSecretNamespace = "default"
	defaultPATTokenKey     = "token"
)

type ClientFactory struct {
	baseHost       string
	baseTLS        rest.TLSClientConfig
	providerConfig *rest.Config

	mu          sync.RWMutex
	hot         map[string]dynamic.Interface
	providerHot map[string]dynamic.Interface
}

func NewClientFactory(base *rest.Config) *ClientFactory {
	if base == nil {
		return nil
	}
	baseHost, err := stripClusterSuffix(base.Host)
	if err != nil {
		baseHost = strings.TrimRight(base.Host, "/")
	}
	providerConfig := rest.CopyConfig(base)
	providerConfig.Host = baseHost

	tls := base.TLSClientConfig
	tls.CertData = nil
	tls.CertFile = ""
	tls.KeyData = nil
	tls.KeyFile = ""
	return &ClientFactory{
		baseHost:       baseHost,
		baseTLS:        tls,
		providerConfig: providerConfig,
		hot:            make(map[string]dynamic.Interface),
		providerHot:    make(map[string]dynamic.Interface),
	}
}

func (f *ClientFactory) For(clusterID, token string) (dynamic.Interface, error) {
	if token == "" {
		return nil, errors.New("no bearer token on request; cannot act on the tenant's behalf")
	}
	key := clusterID + ":" + hashToken(token)

	f.mu.RLock()
	dyn, ok := f.hot[key]
	f.mu.RUnlock()
	if ok {
		return dyn, nil
	}

	cfg := &rest.Config{
		Host:            f.baseHost + "/clusters/" + clusterID,
		BearerToken:     token,
		TLSClientConfig: f.baseTLS,
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client for cluster %q: %w", clusterID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.hot[key]; ok {
		return existing, nil
	}
	if f.hot == nil {
		f.hot = make(map[string]dynamic.Interface)
	}
	f.hot[key] = dyn
	return dyn, nil
}

func (f *ClientFactory) ProviderFor(clusterID string) (dynamic.Interface, error) {
	if strings.TrimSpace(clusterID) == "" {
		return nil, errors.New("no workspace cluster on this request (X-Kedge-Cluster missing)")
	}

	f.mu.RLock()
	dyn, ok := f.providerHot[clusterID]
	f.mu.RUnlock()
	if ok {
		return dyn, nil
	}
	if f.providerConfig == nil {
		return nil, errors.New("provider tenant client unavailable (provider kubeconfig not set)")
	}

	cfg := rest.CopyConfig(f.providerConfig)
	cfg.Host = f.baseHost + "/clusters/" + clusterID
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("provider dynamic client for cluster %q: %w", clusterID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.providerHot[clusterID]; ok {
		return existing, nil
	}
	if f.providerHot == nil {
		f.providerHot = make(map[string]dynamic.Interface)
	}
	f.providerHot[clusterID] = dyn
	return dyn, nil
}

func (f *ClientFactory) TableResolverForRequest(r *http.Request) queryapi.TableResolver {
	if f == nil {
		return queryapi.UnavailableResolver{Message: "tenant client unavailable (provider kubeconfig not set)"}
	}
	ident := identityFromRequest(r)
	return tableResolver{factory: f, identity: ident}
}

type identity struct {
	tenantPath string
	clusterID  string
	token      string
}

func identityFromRequest(r *http.Request) identity {
	id := identity{
		tenantPath: r.Header.Get("X-Kedge-Tenant"),
		clusterID:  r.Header.Get("X-Kedge-Cluster"),
		token:      bearerToken(r),
	}
	if os.Getenv("KEDGE_DEV_ALLOW_TENANT_QUERY") == "true" {
		if id.tenantPath == "" {
			id.tenantPath = r.URL.Query().Get("tenant")
		}
		if id.clusterID == "" {
			id.clusterID = r.URL.Query().Get("cluster")
		}
	}
	return id
}

func bearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

type tableResolver struct {
	factory  *ClientFactory
	identity identity
}

func (r tableResolver) ListTables(ctx context.Context) (map[string]queryapi.TableRef, error) {
	dyn, err := r.dynamicClient()
	if err != nil {
		return nil, err
	}
	list, err := dyn.Resource(tablesGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]queryapi.TableRef, len(list.Items))
	for _, item := range list.Items {
		ref, ok := tableRefFromObject(item)
		if ok {
			out[item.GetName()] = ref
		}
	}
	return out, nil
}

func (r tableResolver) GetTable(ctx context.Context, name string) (queryapi.TableRef, bool, error) {
	dyn, err := r.dynamicClient()
	if err != nil {
		return queryapi.TableRef{}, false, err
	}
	item, err := dyn.Resource(tablesGVR).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return queryapi.TableRef{}, false, nil
	}
	if err != nil {
		return queryapi.TableRef{}, false, err
	}
	ref, ok := tableRefFromObject(*item)
	if !ok {
		return queryapi.TableRef{}, false, nil
	}
	return ref, true, nil
}

func (r tableResolver) GetTableTarget(ctx context.Context, name string) (queryapi.TableTarget, bool, error) {
	callerDyn, err := r.dynamicClient()
	if err != nil {
		return queryapi.TableTarget{}, false, err
	}
	item, err := callerDyn.Resource(tablesGVR).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return queryapi.TableTarget{}, false, nil
	}
	if err != nil {
		return queryapi.TableTarget{}, false, err
	}
	providerDyn, err := r.providerDynamicClient()
	if err != nil {
		return queryapi.TableTarget{}, false, err
	}
	target, ok, err := tableTargetFromObject(ctx, providerDyn, *item)
	return target, ok, err
}

func (r tableResolver) dynamicClient() (dynamic.Interface, error) {
	if r.identity.tenantPath == "" {
		return nil, errors.New("no tenant identity on this request; bearer token did not resolve to a workspace")
	}
	if r.identity.clusterID == "" {
		return nil, errors.New("no workspace cluster on this request (X-Kedge-Cluster missing)")
	}
	if r.factory == nil {
		return nil, errors.New("tenant client unavailable (provider kubeconfig not set)")
	}
	return r.factory.For(r.identity.clusterID, r.identity.token)
}

func (r tableResolver) providerDynamicClient() (dynamic.Interface, error) {
	if r.identity.tenantPath == "" {
		return nil, errors.New("no tenant identity on this request; bearer token did not resolve to a workspace")
	}
	if r.factory == nil {
		return nil, errors.New("tenant client unavailable (provider kubeconfig not set)")
	}
	return r.factory.ProviderFor(r.identity.clusterID)
}

func tableRefFromObject(item unstructured.Unstructured) (queryapi.TableRef, bool) {
	catalog, _, _ := unstructured.NestedString(item.Object, "spec", "catalog")
	schemaName, _, _ := unstructured.NestedString(item.Object, "spec", "schema")
	table, _, _ := unstructured.NestedString(item.Object, "spec", "table")
	if strings.TrimSpace(catalog) == "" || strings.TrimSpace(schemaName) == "" || strings.TrimSpace(table) == "" {
		return queryapi.TableRef{}, false
	}
	return queryapi.TableRef{Catalog: catalog, Schema: schemaName, Table: table}, true
}

func tableTargetFromObject(ctx context.Context, dyn dynamic.Interface, item unstructured.Unstructured) (queryapi.TableTarget, bool, error) {
	tableRef, ok := tableRefFromObject(item)
	if !ok {
		return queryapi.TableTarget{}, false, nil
	}
	connectionRef, _, _ := unstructured.NestedString(item.Object, "spec", "connectionRef")
	warehouseRef, _, _ := unstructured.NestedString(item.Object, "spec", "warehouseRef")
	if strings.TrimSpace(connectionRef) == "" || strings.TrimSpace(warehouseRef) == "" {
		return queryapi.TableTarget{}, false, fmt.Errorf("table %q is missing connectionRef or warehouseRef", item.GetName())
	}

	warehouse, err := dyn.Resource(warehousesGVR).Get(ctx, warehouseRef, metav1.GetOptions{})
	if err != nil {
		return queryapi.TableTarget{}, false, fmt.Errorf("get warehouse %q: %w", warehouseRef, err)
	}
	warehouseConnectionRef, _, _ := unstructured.NestedString(warehouse.Object, "spec", "connectionRef")
	if warehouseConnectionRef != "" && warehouseConnectionRef != connectionRef {
		return queryapi.TableTarget{}, false, fmt.Errorf("table %q connectionRef %q does not match warehouse %q connectionRef %q", item.GetName(), connectionRef, warehouseRef, warehouseConnectionRef)
	}
	warehouseID, _, _ := unstructured.NestedString(warehouse.Object, "spec", "warehouseID")
	if strings.TrimSpace(warehouseID) == "" {
		return queryapi.TableTarget{}, false, fmt.Errorf("warehouse %q is missing warehouseID", warehouseRef)
	}

	connection, err := dyn.Resource(connectionsGVR).Get(ctx, connectionRef, metav1.GetOptions{})
	if err != nil {
		return queryapi.TableTarget{}, false, fmt.Errorf("get connection %q: %w", connectionRef, err)
	}
	host, _, _ := unstructured.NestedString(connection.Object, "spec", "host")
	authType, _, _ := unstructured.NestedString(connection.Object, "spec", "authType")
	secretName, _, _ := unstructured.NestedString(connection.Object, "spec", "secretRef", "name")
	secretNamespace, _, _ := unstructured.NestedString(connection.Object, "spec", "secretRef", "namespace")
	secretKey, _, _ := unstructured.NestedString(connection.Object, "spec", "secretRef", "key")
	if strings.TrimSpace(host) == "" || strings.TrimSpace(authType) == "" || strings.TrimSpace(secretName) == "" {
		return queryapi.TableTarget{}, false, fmt.Errorf("connection %q is missing host, authType, or secretRef.name", connectionRef)
	}
	if secretNamespace == "" {
		secretNamespace = defaultSecretNamespace
	}
	if secretKey == "" {
		secretKey = defaultSecretKey(authType)
	}
	token, err := secretValue(ctx, dyn, secretNamespace, secretName, secretKey)
	if err != nil {
		return queryapi.TableTarget{}, false, err
	}

	return queryapi.TableTarget{
		Table: tableRef,
		Connection: queryapi.ConnectionRef{
			Name:     connectionRef,
			Host:     host,
			AuthType: authType,
		},
		Warehouse: queryapi.WarehouseRef{
			Name:        warehouseRef,
			WarehouseID: warehouseID,
		},
		Credential: queryapi.Credential{BearerToken: token},
	}, true, nil
}

func defaultSecretKey(authType string) string {
	switch databricksv1alpha1.ConnectionAuthType(authType) {
	case databricksv1alpha1.ConnectionAuthPAT:
		return defaultPATTokenKey
	default:
		return defaultPATTokenKey
	}
}

func secretValue(ctx context.Context, dyn dynamic.Interface, namespace, name, key string) (string, error) {
	secret, err := dyn.Resource(secretsGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get credential secret %s/%s: %w", namespace, name, err)
	}
	data, found, _ := unstructured.NestedMap(secret.Object, "data")
	if !found {
		return "", fmt.Errorf("credential secret %s/%s has no data", namespace, name)
	}
	value, ok := data[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("credential secret %s/%s missing key %q", namespace, name, key)
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return string(decoded), nil
	}
	return value, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

func stripClusterSuffix(host string) (string, error) {
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("parse base kubeconfig host %q: %w", host, err)
	}
	idx := strings.Index(u.Path, "/clusters/")
	if idx < 0 {
		return strings.TrimRight(host, "/"), nil
	}
	u.Path = u.Path[:idx]
	return strings.TrimRight(u.String(), "/"), nil
}
