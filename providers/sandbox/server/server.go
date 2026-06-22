/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/faroshq/provider-sandbox/tenant"
)

var devEnvironmentGVR = schema.GroupVersionResource{
	Group:    "sandbox.kedge.faros.sh",
	Version:  "v1alpha1",
	Resource: "devenvironments",
}

const (
	logicalClusterAnnotation = "kcp.io/cluster"
	previewTokenQuery        = "kedgePreviewToken"
	previewTokenTTL          = time.Hour
)

type Server struct {
	runtimeConfig *rest.Config
	runtimeClient kubernetes.Interface
	tenantFactory *tenant.ClientFactory
	previewSigner *previewSigner
	mux           *mux.Router
}

type Options struct {
	PreviewTokenSecret []byte
}

func New(runtimeConfig *rest.Config, tenantFactory *tenant.ClientFactory) http.Handler {
	return NewWithOptions(runtimeConfig, tenantFactory, Options{})
}

func NewWithOptions(runtimeConfig *rest.Config, tenantFactory *tenant.ClientFactory, opts Options) http.Handler {
	var runtimeClient kubernetes.Interface
	if runtimeConfig != nil {
		runtimeClient, _ = kubernetes.NewForConfig(runtimeConfig)
	}
	s := &Server{
		runtimeConfig: runtimeConfig,
		runtimeClient: runtimeClient,
		tenantFactory: tenantFactory,
		previewSigner: newPreviewSigner(opts.PreviewTokenSecret),
		mux:           mux.NewRouter(),
	}
	s.mux.HandleFunc("/healthz", healthz).Methods(http.MethodGet)
	s.mux.HandleFunc("/api/dev-environments/{name}/sync", s.syncDevEnvironment).Methods(http.MethodPost)
	s.mux.HandleFunc("/api/dev-environments/{name}/restart", s.restartDevEnvironment).Methods(http.MethodPost)
	s.mux.HandleFunc("/api/dev-environments/{name}/logs", s.logsDevEnvironment).Methods(http.MethodGet)
	s.mux.HandleFunc("/api/dev-environments/{name}/status", s.statusDevEnvironment).Methods(http.MethodGet)
	s.mux.HandleFunc("/api/dev-environments/{name}/preview-url", s.previewURLDevEnvironment).Methods(http.MethodGet)
	s.mux.PathPrefix("/api/dev-environments/{name}/preview/").HandlerFunc(s.previewDevEnvironment).Methods(http.MethodGet, http.MethodHead)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type previewTokenPayload struct {
	TenantPath     string `json:"tenantPath"`
	ClusterName    string `json:"clusterName"`
	DevEnvironment string `json:"devEnvironment"`
	ExpiresAt      int64  `json:"expiresAt"`
}

type previewSigner struct {
	secret []byte
	now    func() time.Time
}

func newPreviewSigner(secret []byte) *previewSigner {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			sum := sha256.Sum256([]byte(time.Now().String()))
			secret = sum[:]
		}
	}
	return &previewSigner{secret: append([]byte(nil), secret...), now: time.Now}
}

func (s *previewSigner) sign(payload previewTokenPayload) (string, error) {
	payload.ExpiresAt = s.now().Add(previewTokenTTL).Unix()
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func (s *previewSigner) verify(token, name string) (previewTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return previewTokenPayload{}, fmt.Errorf("invalid preview token")
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(parts[0]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, want) {
		return previewTokenPayload{}, fmt.Errorf("invalid preview token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return previewTokenPayload{}, fmt.Errorf("invalid preview token")
	}
	var payload previewTokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return previewTokenPayload{}, fmt.Errorf("invalid preview token")
	}
	if payload.DevEnvironment != name {
		return previewTokenPayload{}, fmt.Errorf("preview token is for a different environment")
	}
	if payload.TenantPath == "" || payload.ClusterName == "" {
		return previewTokenPayload{}, fmt.Errorf("preview token is incomplete")
	}
	if s.now().Unix() > payload.ExpiresAt {
		return previewTokenPayload{}, fmt.Errorf("preview token expired")
	}
	return payload, nil
}

type identity struct {
	tenantPath string
	token      string
}

func identityFromRequest(w http.ResponseWriter, r *http.Request) (identity, bool) {
	tenantPath := strings.TrimSpace(r.Header.Get("X-Kedge-Tenant"))
	if tenantPath == "" {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "tenant context missing")
		return identity{}, false
	}
	token := bearerToken(r)
	if token == "" {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "bearer token missing")
		return identity{}, false
	}
	return identity{tenantPath: tenantPath, token: token}, true
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("bearer "):])
	}
	return auth
}

func (s *Server) devEnvironment(w http.ResponseWriter, r *http.Request, id identity, name string) (*unstructured.Unstructured, bool) {
	dyn, err := s.tenantFactory.For(id.tenantPath, id.token)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return nil, false
	}
	obj, err := dyn.Resource(devEnvironmentGVR).Get(r.Context(), name, metav1.GetOptions{})
	if err != nil {
		writeStatus(w, http.StatusNotFound, "NotFound", err.Error())
		return nil, false
	}
	return obj, true
}

func runtimeNamespace(tenantPath string) string {
	sum := sha256.Sum256([]byte(tenantPath))
	return "sandbox-" + hex.EncodeToString(sum[:])[:16]
}

func runtimeClusterName(tenantPath string, obj *unstructured.Unstructured) string {
	if obj != nil {
		if cluster := strings.TrimSpace(obj.GetAnnotations()[logicalClusterAnnotation]); cluster != "" {
			return cluster
		}
	}
	return tenantPath
}

func serviceName(name string) string {
	return name + "-preview"
}

func controlSecretName(name string) string {
	return name + "-control"
}

func writeStatus(w http.ResponseWriter, code int, reason, message string) {
	writeJSON(w, code, map[string]any{
		"kind":       "Status",
		"apiVersion": "v1",
		"metadata":   map[string]any{},
		"status":     "Failure",
		"message":    message,
		"reason":     reason,
		"code":       code,
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
