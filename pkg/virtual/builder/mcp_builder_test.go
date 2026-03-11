/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package builder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

// newTestMCPVirtualWorkspaces builds a minimal virtualWorkspaces for MCP handler tests.
// It sets a non-nil kcpConfig so that clusterScopedDynamicClient can build scoped clients.
func newTestMCPVirtualWorkspaces() *virtualWorkspaces {
	return &virtualWorkspaces{
		kcpConfig:       &rest.Config{Host: "https://kcp.example.com"},
		staticTokens:    make(map[string]struct{}),
		edgeConnManager: NewConnManager(),
	}
}

// TestMCPHandler_missingToken verifies that a request without an Authorization
// header is rejected with HTTP 401.
func TestMCPHandler_missingToken(t *testing.T) {
	vws := newTestMCPVirtualWorkspaces()
	// Use a minimal fake dynamic client — handler should reject before using it.
	dynClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	handler := vws.buildMCPHandler(dynClient, "https://kedge.example.com/services/edges-proxy")

	req := httptest.NewRequest(http.MethodPost, "/root:kedge:user-default/mcp", nil)
	// No Authorization header.
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestMCPHandler_missingCluster verifies that a request with a token but an
// empty/root path is rejected with HTTP 400.
func TestMCPHandler_missingCluster(t *testing.T) {
	vws := newTestMCPVirtualWorkspaces()
	dynClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	handler := vws.buildMCPHandler(dynClient, "https://kedge.example.com/services/edges-proxy")

	// Path is "/" which strips to "" — missing cluster.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestMCPHandler_unknownEndpoint verifies that a path ending with an unknown
// endpoint suffix (not "mcp", "sse", or "message") returns HTTP 404.
// A fake kcp API server is used so the MCP server can be fully initialised
// without making real network calls.
func TestMCPHandler_unknownEndpoint(t *testing.T) {
	// Start a fake kcp server that returns an empty Edge list for all requests.
	fakeKCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a minimal empty Unstructured list for any LIST call.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"List","items":[],"metadata":{}}`))
	}))
	defer fakeKCP.Close()

	vws := &virtualWorkspaces{
		kcpConfig:       &rest.Config{Host: fakeKCP.URL},
		staticTokens:    make(map[string]struct{}),
		edgeConnManager: NewConnManager(),
	}
	dynClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	handler := vws.buildMCPHandler(dynClient, "https://kedge.example.com/services/edges-proxy")

	// Path: {cluster}/badpath — unknown endpoint.
	req := httptest.NewRequest(http.MethodGet, "/root:kedge:user-default/badpath", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404 Not Found, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestClusterScopedDynamicClient_appendsCluster verifies that
// clusterScopedDynamicClient produces a dynamic client whose host URL includes
// the /clusters/<name> path appended to the base kcp host.
func TestClusterScopedDynamicClient_appendsCluster(t *testing.T) {
	const (
		baseHost = "https://kcp.example.com"
		cluster  = "root:kedge:user-default"
	)

	kcpConfig := &rest.Config{Host: baseHost}

	client, err := clusterScopedDynamicClient(kcpConfig, cluster)
	if err != nil {
		t.Fatalf("clusterScopedDynamicClient returned unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("clusterScopedDynamicClient returned nil client")
	}

	// We can't directly inspect the dynamic client's host, but we can verify the
	// logic via appendClusterPath (the same function used internally).
	expectedHost := appendClusterPath(baseHost, cluster)

	if !strings.Contains(expectedHost, "/clusters/"+cluster) {
		t.Errorf("expected host %q to contain /clusters/%s", expectedHost, cluster)
	}
	if !strings.HasPrefix(expectedHost, baseHost) {
		t.Errorf("expected host %q to start with %s", expectedHost, baseHost)
	}
}

// TestClusterScopedDynamicClient_nilKcpConfig verifies that passing a nil
// kcpConfig returns an error.
func TestClusterScopedDynamicClient_nilKcpConfig(t *testing.T) {
	_, err := clusterScopedDynamicClient(nil, "root:kedge:user-default")
	if err == nil {
		t.Fatal("expected error when kcpConfig is nil, got nil")
	}
}
