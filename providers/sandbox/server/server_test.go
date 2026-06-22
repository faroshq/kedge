/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRuntimeClusterNamePrefersLogicalClusterAnnotation(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetAnnotations(map[string]string{logicalClusterAnnotation: "tenant-cluster-id"})
	if got, want := runtimeClusterName("root:kedge:tenants:org:ws", obj), "tenant-cluster-id"; got != want {
		t.Fatalf("runtimeClusterName = %q, want %q", got, want)
	}
}

func TestRuntimeClusterNameFallsBackToTenantPath(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetCreationTimestamp(metav1.Now())
	if got, want := runtimeClusterName("root:kedge:tenants:org:ws", obj), "root:kedge:tenants:org:ws"; got != want {
		t.Fatalf("runtimeClusterName = %q, want %q", got, want)
	}
}

func TestRuntimeServicePathUsesSanitizedSuffix(t *testing.T) {
	got := runtimeServicePath("tenant-cluster-id", "todo-dev", "preview", "../src/")
	if strings.Contains(got, "..") {
		t.Fatalf("runtimeServicePath = %q, should not contain traversal segments", got)
	}
	if want := "/api/v1/namespaces/" + runtimeNamespace("tenant-cluster-id") + "/services/todo-dev-preview:preview/proxy/src"; got != want {
		t.Fatalf("runtimeServicePath = %q, want %q", got, want)
	}
}

func TestSyncResponseWithPreviewURL(t *testing.T) {
	s := NewWithOptions(nil, nil, Options{PreviewTokenSecret: []byte("test-secret")}).(*Server)
	raw := s.syncResponseWithPreviewURL([]byte(`{"phase":"Synced","changed":["src/App.vue"]}`), "root:kedge:tenants:org:ws", "logical-cluster", "todo-dev")
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("syncResponseWithPreviewURL returned invalid JSON: %v", err)
	}
	previewURL, ok := got["previewURL"].(string)
	if !ok {
		t.Fatalf("previewURL = %#v, want string", got["previewURL"])
	}
	u, err := url.Parse(previewURL)
	if err != nil {
		t.Fatalf("parse previewURL: %v", err)
	}
	if got, want := u.Path, "/services/providers/sandbox/api/dev-environments/todo-dev/preview/"; got != want {
		t.Fatalf("previewURL path = %q, want %q", got, want)
	}
	payload, err := s.previewSigner.verify(u.Query().Get(previewTokenQuery), "todo-dev")
	if err != nil {
		t.Fatalf("preview token did not verify: %v", err)
	}
	if got, want := payload.TenantPath, "root:kedge:tenants:org:ws"; got != want {
		t.Fatalf("preview token tenant = %q, want %q", got, want)
	}
	if got, want := payload.ClusterName, "logical-cluster"; got != want {
		t.Fatalf("preview token cluster = %q, want %q", got, want)
	}
}

func TestPreviewTokenFromRequestSetsScopedCookieAndRedirects(t *testing.T) {
	s := NewWithOptions(nil, nil, Options{PreviewTokenSecret: []byte("test-secret")}).(*Server)
	token, err := s.previewSigner.sign(previewTokenPayload{
		TenantPath:     "root:kedge:tenants:org:ws",
		ClusterName:    "logical-cluster",
		DevEnvironment: "todo-dev",
	})
	if err != nil {
		t.Fatalf("sign preview token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/dev-environments/todo-dev/preview/?"+previewTokenQuery+"="+url.QueryEscape(token)+"&view=full", nil)
	req.Header.Set("X-Kedge-Tenant", "root:kedge:tenants:org:ws")
	rec := httptest.NewRecorder()

	if _, _, ok := s.previewTokenFromRequest(rec, req, "todo-dev"); ok {
		t.Fatal("previewTokenFromRequest returned ok for bootstrap redirect")
	}
	resp := rec.Result()
	if got, want := resp.StatusCode, http.StatusFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	scope := previewTokenScope(token)
	if got, want := resp.Header.Get("Location"), "/services/providers/sandbox/api/dev-environments/todo-dev/preview/"+previewScopePrefix+"/"+scope+"/?view=full"; got != want {
		t.Fatalf("redirect location = %q, want %q", got, want)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one scoped preview cookie", cookies)
	}
	if got, want := cookies[0].Name, previewCookieName("todo-dev", scope); got != want {
		t.Fatalf("cookie name = %q, want %q", got, want)
	}
	if got, want := cookies[0].Path, "/services/providers/sandbox/api/dev-environments/todo-dev/preview/"+previewScopePrefix+"/"+scope+"/"; got != want {
		t.Fatalf("cookie path = %q, want %q", got, want)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/dev-environments/todo-dev/preview/"+previewScopePrefix+"/"+scope+"/app.css", nil)
	req2.AddCookie(cookies[0])
	payload, suffix, ok := s.previewTokenFromRequest(httptest.NewRecorder(), req2, "todo-dev")
	if !ok {
		t.Fatal("previewTokenFromRequest returned !ok")
	}
	if got, want := payload.TenantPath, "root:kedge:tenants:org:ws"; got != want {
		t.Fatalf("tenant = %q, want %q", got, want)
	}
	if got, want := suffix, "app.css"; got != want {
		t.Fatalf("runtime suffix = %q, want %q", got, want)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/dev-environments/todo-dev/preview/app.css", nil)
	req3.AddCookie(cookies[0])
	if _, _, ok := s.previewTokenFromRequest(httptest.NewRecorder(), req3, "todo-dev"); ok {
		t.Fatal("unscoped preview cookie was accepted")
	}
}

func TestPreviewTargetSignedTokenTakesPrecedenceOverPartialHubHeaders(t *testing.T) {
	s := NewWithOptions(nil, nil, Options{PreviewTokenSecret: []byte("test-secret")}).(*Server)
	token, err := s.previewSigner.sign(previewTokenPayload{
		TenantPath:     "root:kedge:tenants:org:ws",
		ClusterName:    "logical-cluster",
		DevEnvironment: "todo-dev",
	})
	if err != nil {
		t.Fatalf("sign preview token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/dev-environments/todo-dev/preview/?"+previewTokenQuery+"="+url.QueryEscape(token), nil)
	req.Header.Set("X-Kedge-Tenant", "root:kedge:tenants:org:ws")
	rec := httptest.NewRecorder()

	if _, _, ok := s.previewTarget(rec, req, "todo-dev"); ok {
		t.Fatal("previewTarget returned ok for bootstrap redirect")
	}
	if got, want := rec.Result().StatusCode, http.StatusFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestPreviewRuntimeRawQueryStripsPreviewToken(t *testing.T) {
	query := url.Values{}
	query.Set(previewTokenQuery, "secret")
	query.Set("page", "1")
	query.Add("filter", "ready")
	query.Add("filter", "pending")

	got := previewRuntimeRawQuery(query)
	if strings.Contains(got, previewTokenQuery) || strings.Contains(got, "secret") {
		t.Fatalf("previewRuntimeRawQuery leaked preview token: %q", got)
	}
	values, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if got, want := values.Get("page"), "1"; got != want {
		t.Fatalf("page = %q, want %q", got, want)
	}
	if got, want := strings.Join(values["filter"], ","), "ready,pending"; got != want {
		t.Fatalf("filter = %q, want %q", got, want)
	}
}

func TestStripPreviewForwardedCredentials(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer secret")
	header.Set("Cookie", "session=secret")
	header.Set("X-Kedge-Tenant", "root:tenant")
	header.Set("X-Kedge-User", "user")
	header.Set("X-Kedge-Extra", "secret")
	header.Set("X-Sandbox-Control-Token", "secret")
	header.Set("X-Trace-ID", "keep")

	stripPreviewForwardedCredentials(header)

	for _, key := range []string{"Authorization", "Cookie", "X-Kedge-Tenant", "X-Kedge-User", "X-Kedge-Extra", "X-Sandbox-Control-Token"} {
		if header.Get(key) != "" {
			t.Fatalf("%s was not stripped", key)
		}
	}
	if header.Get("X-Trace-ID") != "keep" {
		t.Fatalf("X-Trace-ID = %q, want keep", header.Get("X-Trace-ID"))
	}
}
