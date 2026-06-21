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
	raw := syncResponseWithPreviewURL([]byte(`{"phase":"Synced","changed":["src/App.vue"]}`), "todo-dev")
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("syncResponseWithPreviewURL returned invalid JSON: %v", err)
	}
	if got["previewURL"] != "/services/providers/sandbox/api/dev-environments/todo-dev/preview/" {
		t.Fatalf("previewURL = %v", got["previewURL"])
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
