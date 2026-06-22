/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const previewScopePrefix = "__kedge_preview"

type previewURLResponse struct {
	Ready      bool   `json:"ready"`
	PreviewURL string `json:"previewURL,omitempty"`
	Message    string `json:"message,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (s *Server) previewURLDevEnvironment(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFromRequest(w, r)
	if !ok {
		return
	}
	name := mux.Vars(r)["name"]
	env, ok := s.devEnvironment(w, r, id, name)
	if !ok {
		return
	}
	clusterName := runtimeClusterName(id.tenantPath, env)
	readiness := s.previewReadiness(r.Context(), clusterName, name)
	if !readiness.Ready {
		writeJSON(w, http.StatusOK, readiness)
		return
	}
	readiness.PreviewURL = s.signedPreviewURL(id.tenantPath, clusterName, name)
	writeJSON(w, http.StatusOK, readiness)
}

func (s *Server) previewReadiness(ctx context.Context, clusterName, name string) previewURLResponse {
	if s.runtimeClient == nil {
		return previewURLResponse{
			Ready:   false,
			Reason:  "runtime_not_configured",
			Message: "Preview is getting ready. The sandbox runtime is still being configured.",
		}
	}
	endpoints, err := s.runtimeClient.CoreV1().Endpoints(runtimeNamespace(clusterName)).Get(ctx, serviceName(name), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return previewURLResponse{
			Ready:   false,
			Reason:  "service_not_found",
			Message: "Preview is getting ready. The preview service has not been created yet.",
		}
	}
	if err != nil {
		return previewURLResponse{
			Ready:   false,
			Reason:  "service_unavailable",
			Message: "Preview is getting ready. The sandbox runtime is not reachable yet.",
		}
	}
	if !hasReadyEndpoint(endpoints) {
		return previewURLResponse{
			Ready:   false,
			Reason:  "no_ready_endpoints",
			Message: "Preview is getting ready. The sandbox runtime is not serving traffic yet.",
		}
	}
	return previewURLResponse{Ready: true}
}

func hasReadyEndpoint(endpoints *corev1.Endpoints) bool {
	if endpoints == nil {
		return false
	}
	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) > 0 {
			return true
		}
	}
	return false
}

func (s *Server) previewDevEnvironment(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	clusterName, suffix, ok := s.previewTarget(w, r, name)
	if !ok {
		return
	}
	if s.runtimeConfig == nil {
		writeStatus(w, http.StatusNotImplemented, "NotImplemented", "runtime kubeconfig not configured")
		return
	}
	target, err := url.Parse(s.runtimeConfig.Host)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	transport, err := restTransport(s.runtimeConfig)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = runtimeServicePath(clusterName, name, "preview", suffix)
		req.URL.RawQuery = previewRuntimeRawQuery(r.URL.Query())
		req.Host = target.Host
		stripPreviewForwardedCredentials(req.Header)
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) previewTarget(w http.ResponseWriter, r *http.Request, name string) (string, string, bool) {
	if r.URL.Query().Get(previewTokenQuery) != "" || previewRequestScope(name, r.URL.Path) != "" {
		payload, suffix, ok := s.previewTokenFromRequest(w, r, name)
		if !ok {
			return "", "", false
		}
		return payload.ClusterName, suffix, true
	}
	if strings.TrimSpace(r.Header.Get("X-Kedge-Tenant")) != "" || bearerToken(r) != "" {
		id, ok := identityFromRequest(w, r)
		if !ok {
			return "", "", false
		}
		env, ok := s.devEnvironment(w, r, id, name)
		if !ok {
			return "", "", false
		}
		return runtimeClusterName(id.tenantPath, env), previewRuntimeSuffix(name, r.URL.Path), true
	}
	writeStatus(w, http.StatusUnauthorized, "Unauthorized", "tenant context missing")
	return "", "", false
}

func (s *Server) previewTokenFromRequest(w http.ResponseWriter, r *http.Request, name string) (previewTokenPayload, string, bool) {
	token := strings.TrimSpace(r.URL.Query().Get(previewTokenQuery))
	if token != "" {
		payload, err := s.previewSigner.verify(token, name)
		if err != nil {
			writeStatus(w, http.StatusUnauthorized, "Unauthorized", err.Error())
			return previewTokenPayload{}, "", false
		}
		scope := previewTokenScope(token)
		setPreviewTokenCookie(w, name, scope, token, time.Unix(payload.ExpiresAt, 0))
		http.Redirect(w, r, scopedPreviewRedirectURL(name, scope, r.URL.Query()), http.StatusFound)
		return previewTokenPayload{}, "", false
	}
	scope, suffix, ok := previewRequestScopeAndSuffix(name, r.URL.Path)
	if !ok {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "tenant context missing")
		return previewTokenPayload{}, "", false
	}
	cookie, err := r.Cookie(previewCookieName(name, scope))
	if err != nil {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "tenant context missing")
		return previewTokenPayload{}, "", false
	}
	if previewTokenScope(cookie.Value) != scope {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "invalid preview token")
		return previewTokenPayload{}, "", false
	}
	payload, err := s.previewSigner.verify(cookie.Value, name)
	if err != nil {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", err.Error())
		return previewTokenPayload{}, "", false
	}
	return payload, suffix, true
}

func setPreviewTokenCookie(w http.ResponseWriter, name, scope, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     previewCookieName(name, scope),
		Value:    token,
		Path:     externalScopedPreviewPath(name, scope),
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func previewCookieName(name, scope string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + scope))
	return "kedge_sandbox_preview_" + hex.EncodeToString(sum[:])[:16]
}

func externalPreviewPath(name string) string {
	return "/services/providers/sandbox/api/dev-environments/" + name + "/preview/"
}

func externalScopedPreviewPath(name, scope string) string {
	return externalPreviewPath(name) + previewScopePrefix + "/" + scope + "/"
}

func scopedPreviewRedirectURL(name, scope string, query url.Values) string {
	target := externalScopedPreviewPath(name, scope)
	if raw := previewRuntimeRawQuery(query); raw != "" {
		target += "?" + raw
	}
	return target
}

func previewTokenScope(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:16]
}

func previewRequestScope(name, requestPath string) string {
	scope, _, ok := previewRequestScopeAndSuffix(name, requestPath)
	if !ok {
		return ""
	}
	return scope
}

func previewRequestScopeAndSuffix(name, requestPath string) (string, string, bool) {
	suffix := previewRuntimeSuffix(name, requestPath)
	segment := previewScopePrefix + "/"
	if !strings.HasPrefix(suffix, segment) {
		return "", suffix, false
	}
	rest := strings.TrimPrefix(suffix, segment)
	scope, next, found := strings.Cut(rest, "/")
	if !found || !validPreviewScope(scope) {
		return "", suffix, false
	}
	return scope, next, true
}

func previewRuntimeSuffix(name, requestPath string) string {
	prefix := "/api/dev-environments/" + name + "/preview/"
	return strings.TrimPrefix(requestPath, prefix)
}

func validPreviewScope(scope string) bool {
	if len(scope) != 16 {
		return false
	}
	for _, ch := range scope {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func previewRuntimeRawQuery(query url.Values) string {
	if _, ok := query[previewTokenQuery]; !ok {
		return query.Encode()
	}
	next := make(url.Values, len(query))
	for key, values := range query {
		if key == previewTokenQuery {
			continue
		}
		next[key] = append([]string(nil), values...)
	}
	return next.Encode()
}

func stripPreviewForwardedCredentials(header http.Header) {
	header.Del("Authorization")
	header.Del("Cookie")
	header.Del("X-Kedge-Tenant")
	header.Del("X-Kedge-User")
	header.Del("X-Sandbox-Control-Token")
	for key := range header {
		if strings.HasPrefix(strings.ToLower(key), "x-kedge-") {
			header.Del(key)
		}
	}
}

func runtimeServicePath(tenantPath, name, portName, suffix string) string {
	cleanSuffix := strings.TrimPrefix(path.Clean("/"+suffix), "/")
	base := "/api/v1/namespaces/" + runtimeNamespace(tenantPath) + "/services/" + serviceName(name) + ":" + portName + "/proxy"
	if cleanSuffix == "" || cleanSuffix == "." {
		return base + "/"
	}
	return base + "/" + cleanSuffix
}

func restTransport(cfg *rest.Config) (http.RoundTripper, error) {
	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return nil, err
	}
	return transport, nil
}
