/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package httpapi serves the code provider's small read-only HTTP surface that
// the portal cannot get straight from kcp. Today that is one route — the
// packages list — reached through the hub's authenticated /services proxy at
// /services/providers/code/packages.
//
// Why this can't be a CRD like Connection/Repository: GitHub Packages are
// observed state (artifacts published by `docker push` / `npm publish`), not
// desired state, and the portal's browser token cannot call GitHub directly.
// So the request flows: portal → hub /services proxy (injects identity) → this
// handler, which resolves the caller's Repository → Connection → credential in
// their tenant workspace AS THE CALLER (never as the provider) and queries the
// host. Authorization is entirely kcp's: every read uses the caller's own
// bearer token, so a `tenant` the caller cannot reach simply 403s.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"

	codev1alpha1 "github.com/faroshq/faros-kedge/providers/code/apis/v1alpha1"
	"github.com/faroshq/faros-kedge/providers/code/backend"
	"github.com/faroshq/faros-kedge/providers/code/tenant"
)

var (
	connectionsGVR  = codev1alpha1.SchemeGroupVersion.WithResource("connections")
	repositoriesGVR = codev1alpha1.SchemeGroupVersion.WithResource("repositories")
)

// PackagesHandler serves GET /packages?repo=<name>[&tenant=<path>].
type PackagesHandler struct {
	tenant   *tenant.ClientFactory
	backends *backend.Registry
}

// NewPackagesHandler wires the handler. tenant may be nil (serve mode with no
// kcp config) — the handler then reports the dependency as unavailable rather
// than panicking.
func NewPackagesHandler(t *tenant.ClientFactory, backends *backend.Registry) *PackagesHandler {
	return &PackagesHandler{tenant: t, backends: backends}
}

// packageOut is one package in the JSON response (mirrors backend.PackageInfo).
type packageOut struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Visibility   string `json:"visibility,omitempty"`
	HTMLURL      string `json:"htmlURL,omitempty"`
	VersionCount int64  `json:"versionCount,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

// packagesResponse is the shape the portal parses. Supported is false when the
// repository's connection provider has no package-listing capability, so the
// view can say "not supported" rather than "none".
type packagesResponse struct {
	Supported bool         `json:"supported"`
	Packages  []packageOut `json:"packages"`
}

// errorResponse matches the portal's {reason, message} contract (api.ts).
type errorResponse struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// ServeHTTP handles the packages list. Mounted at the exact path /packages.
func (h *PackagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "only GET is supported")
		return
	}

	repoName := r.URL.Query().Get("repo")
	if repoName == "" {
		writeErr(w, http.StatusBadRequest, "BadRequest", "missing required query parameter: repo")
		return
	}
	tenantPath, token := identityFromRequest(r)
	if h.tenant == nil {
		writeErr(w, http.StatusServiceUnavailable, "Unavailable", "tenant client unavailable (provider kubeconfig not set)")
		return
	}
	if tenantPath == "" {
		writeErr(w, http.StatusBadRequest, "TenantMissing", "no workspace on request — pass ?tenant or X-Kedge-Tenant")
		return
	}
	if token == "" {
		writeErr(w, http.StatusUnauthorized, "Unauthorized", "no bearer token on request")
		return
	}

	dyn, err := h.tenant.For(tenantPath, token)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "TenantClient", err.Error())
		return
	}

	ctx := r.Context()
	conn, repo, err := resolveRepoAndConnection(ctx, dyn, repoName)
	if err != nil {
		status, reason := mapK8sError(err)
		writeErr(w, status, reason, err.Error())
		return
	}

	b, ok := h.backends.Get(string(conn.Spec.Provider))
	if !ok {
		// Provider not registered in this process: treat as "no packages".
		writeJSON(w, http.StatusOK, packagesResponse{Supported: false, Packages: []packageOut{}})
		return
	}
	lister, ok := b.(backend.PackageLister)
	if !ok {
		writeJSON(w, http.StatusOK, packagesResponse{Supported: false, Packages: []packageOut{}})
		return
	}

	tok, err := tenant.ResolveToken(ctx, dyn, conn.Spec.SecretRef.Namespace, conn.Spec.SecretRef.Name, conn.Spec.SecretRef.Key)
	if err != nil {
		status, reason := mapK8sError(err)
		writeErr(w, status, reason, err.Error())
		return
	}

	infos, err := lister.ListPackages(ctx, conn, backend.Credential{Token: tok}, repo)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "HostError", err.Error())
		return
	}

	resp := packagesResponse{Supported: true, Packages: make([]packageOut, 0, len(infos))}
	for _, p := range infos {
		resp.Packages = append(resp.Packages, packageOut(p))
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveRepoAndConnection loads the named Repository and its referenced
// Connection as typed objects, using the caller-scoped dynamic client.
func resolveRepoAndConnection(ctx context.Context, dyn dynamic.Interface, repoName string) (*codev1alpha1.Connection, *codev1alpha1.Repository, error) {
	repoU, err := dyn.Resource(repositoriesGVR).Get(ctx, repoName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	var repo codev1alpha1.Repository
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(repoU.Object, &repo); err != nil {
		return nil, nil, err
	}
	if repo.Spec.ConnectionRef == "" {
		return nil, nil, errors.New("repository has no connectionRef")
	}

	connU, err := dyn.Resource(connectionsGVR).Get(ctx, repo.Spec.ConnectionRef, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	var conn codev1alpha1.Connection
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(connU.Object, &conn); err != nil {
		return nil, nil, err
	}
	return &conn, &repo, nil
}

// identityFromRequest extracts the tenant workspace path and caller bearer
// token. The hub /services proxy injects X-Kedge-Tenant after authenticating;
// the portal also passes ?tenant=<selected workspace> because the proxy header
// reflects the user's home workspace, not whichever one the portal is viewing.
// Honoring the query param is safe: every kcp read still authenticates with the
// caller's own token, so an out-of-reach tenant simply 403s. The token query
// fallback is dev-only (KEDGE_DEV_ALLOW_TENANT_QUERY), matching the MCP surface.
func identityFromRequest(r *http.Request) (tenantPath, token string) {
	tenantPath = r.URL.Query().Get("tenant")
	if tenantPath == "" {
		tenantPath = r.Header.Get("X-Kedge-Tenant")
	}
	token = bearerToken(r)
	if token == "" && os.Getenv("KEDGE_DEV_ALLOW_TENANT_QUERY") == "true" {
		token = r.URL.Query().Get("token")
	}
	return tenantPath, token
}

func bearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// mapK8sError turns a kcp/apimachinery error into an HTTP status + portal
// reason, mirroring api.ts's NotFound/APIBindingMissing handling.
func mapK8sError(err error) (int, string) {
	switch {
	case apierrors.IsNotFound(err) || errors.Is(err, tenant.ErrCredentialsMissing):
		return http.StatusNotFound, "NotFound"
	case apierrors.IsForbidden(err) || errors.Is(err, tenant.ErrAPIBindingMissing):
		return http.StatusForbidden, "APIBindingMissing"
	case apierrors.IsUnauthorized(err):
		return http.StatusUnauthorized, "Unauthorized"
	default:
		return http.StatusInternalServerError, "ServerError"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, reason, message string) {
	writeJSON(w, status, errorResponse{Reason: reason, Message: message})
}
