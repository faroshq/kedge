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
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

// makeEdgeUnstructured creates a minimal Unstructured Edge object with the given name.
func makeEdgeUnstructured(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(edgeGVRMCP.GroupVersion().WithKind("Edge"))
	u.SetName(name)
	u.SetNamespace("")
	return u
}

// newFakeDynamicClientWithEdges builds a fake dynamic client pre-populated with
// the given Edge objects.
func newFakeDynamicClientWithEdges(edges ...*unstructured.Unstructured) *fake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	objects := make([]runtime.Object, 0, len(edges))
	for _, e := range edges {
		objects = append(objects, e)
	}
	return fake.NewSimpleDynamicClient(scheme, objects...)
}

// TestGetTargets_returnsOnlyConnectedEdges verifies that GetTargets returns only
// the edges whose tunnel key is present in ConnManager.
func TestGetTargets_returnsOnlyConnectedEdges(t *testing.T) {
	const cluster = "root:kedge:user-default"

	edge1 := makeEdgeUnstructured("edge-one")
	edge2 := makeEdgeUnstructured("edge-two")
	edge3 := makeEdgeUnstructured("edge-three")

	dynClient := newFakeDynamicClientWithEdges(edge1, edge2, edge3)

	cm := NewConnManager()
	// Register only edge-one and edge-two in the ConnManager.
	cm.Store(edgeConnKey(cluster, "edge-one"), nil)
	cm.Store(edgeConnKey(cluster, "edge-two"), nil)

	provider := &KedgeEdgeProvider{
		cluster:         cluster,
		edgeConnManager: cm,
		dynamicClient:   dynClient,
		edgeProxyBase:   "https://kedge.example.com/services/edges-proxy",
		bearerToken:     "test-token",
	}

	targets, err := provider.GetTargets(context.Background())
	if err != nil {
		t.Fatalf("GetTargets returned unexpected error: %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %v", len(targets), targets)
	}

	targetSet := make(map[string]bool)
	for _, tgt := range targets {
		targetSet[tgt] = true
	}

	if !targetSet["edge-one"] {
		t.Errorf("expected edge-one in targets, got %v", targets)
	}
	if !targetSet["edge-two"] {
		t.Errorf("expected edge-two in targets, got %v", targets)
	}
	if targetSet["edge-three"] {
		t.Errorf("edge-three should NOT be in targets (no tunnel), got %v", targets)
	}
}

// TestGetTargets_emptyWhenNoneConnected verifies that GetTargets returns an
// empty (nil) slice when no tunnels are registered in ConnManager.
func TestGetTargets_emptyWhenNoneConnected(t *testing.T) {
	const cluster = "root:kedge:user-default"

	edge1 := makeEdgeUnstructured("edge-alpha")
	edge2 := makeEdgeUnstructured("edge-beta")

	dynClient := newFakeDynamicClientWithEdges(edge1, edge2)

	cm := NewConnManager() // empty — no tunnels

	provider := &KedgeEdgeProvider{
		cluster:         cluster,
		edgeConnManager: cm,
		dynamicClient:   dynClient,
		edgeProxyBase:   "https://kedge.example.com/services/edges-proxy",
		bearerToken:     "test-token",
	}

	targets, err := provider.GetTargets(context.Background())
	if err != nil {
		t.Fatalf("GetTargets returned unexpected error: %v", err)
	}

	if len(targets) != 0 {
		t.Fatalf("expected 0 targets, got %d: %v", len(targets), targets)
	}
}

// TestGetDerivedKubernetes_correctURL verifies that GetDerivedKubernetes builds a
// Kubernetes client whose Host matches the expected edges-proxy URL format.
func TestGetDerivedKubernetes_correctURL(t *testing.T) {
	const (
		cluster       = "root:kedge:user-default"
		edgeName      = "myedge"
		edgeProxyBase = "https://kedge.example.com/services/edges-proxy"
		bearerToken   = "user-bearer-token"
	)

	cm := NewConnManager()
	dynClient := newFakeDynamicClientWithEdges()

	provider := &KedgeEdgeProvider{
		cluster:         cluster,
		edgeConnManager: cm,
		dynamicClient:   dynClient,
		edgeProxyBase:   edgeProxyBase,
		bearerToken:     bearerToken,
	}

	k8s, err := provider.GetDerivedKubernetes(context.Background(), edgeName)
	if err != nil {
		t.Fatalf("GetDerivedKubernetes returned unexpected error: %v", err)
	}
	if k8s == nil {
		t.Fatal("GetDerivedKubernetes returned nil Kubernetes")
	}

	// The expected URL matches the format described in KedgeEdgeProvider.GetDerivedKubernetes.
	expectedURL := edgeProxyBase + "/clusters/" + cluster + "/apis/kedge.faros.sh/v1alpha1/edges/" + edgeName + "/k8s"

	// Retrieve the REST config from the Kubernetes client to inspect Host.
	restCfg := k8s.RESTConfig()
	if restCfg == nil {
		t.Fatal("k8s.RESTConfig() returned nil")
	}

	if restCfg.Host != expectedURL {
		t.Errorf("expected Host to be %q, got %q", expectedURL, restCfg.Host)
	}
}

// TestGetDefaultTarget asserts that GetDefaultTarget returns an empty string.
func TestGetDefaultTarget(t *testing.T) {
	provider := &KedgeEdgeProvider{}
	if got := provider.GetDefaultTarget(); got != "" {
		t.Errorf("GetDefaultTarget() = %q; want empty string", got)
	}
}

// TestGetTargetParameterName asserts that GetTargetParameterName returns "edge".
func TestGetTargetParameterName(t *testing.T) {
	provider := &KedgeEdgeProvider{}
	if got := provider.GetTargetParameterName(); got != "edge" {
		t.Errorf("GetTargetParameterName() = %q; want %q", got, "edge")
	}
}
