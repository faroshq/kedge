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
	"fmt"

	mcpapi "github.com/containers/kubernetes-mcp-server/pkg/api"
	mcpkubernetes "github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// edgeGVRMCP is the GVR used to list Edge objects when building MCP targets.
// It mirrors edgeGVR from edge_status.go but is defined here to avoid
// cross-file init-order issues.
var edgeGVRMCP = schema.GroupVersionResource{
	Group:    "kedge.faros.sh",
	Version:  "v1alpha1",
	Resource: "edges",
}

// KedgeEdgeProvider implements the kubernetes-mcp-server Provider interface so
// that the MCP server can enumerate all connected Edge tunnels for a tenant and
// proxy Kubernetes API calls through them.
type KedgeEdgeProvider struct {
	cluster         string            // kcp cluster path, e.g. "root:kedge:user-default"
	edgeConnManager *ConnManager      // shared dialer registry
	dynamicClient   dynamic.Interface // cluster-scoped dynamic client for listing Edge objects
	edgeProxyBase   string            // e.g. "https://kedge.example.com/services/edges-proxy"
	bearerToken     string            // caller's bearer token to forward to edge proxies
}

// Ensure KedgeEdgeProvider implements mcpkubernetes.Provider.
var _ mcpkubernetes.Provider = (*KedgeEdgeProvider)(nil)

// IsOpenShift always returns false — kedge edges are vanilla Kubernetes clusters.
func (p *KedgeEdgeProvider) IsOpenShift(_ context.Context) bool {
	return false
}

// GetTargets lists all Edge objects in the tenant cluster and filters to those
// that have an active reverse tunnel registered in ConnManager.
func (p *KedgeEdgeProvider) GetTargets(ctx context.Context) ([]string, error) {
	list, err := p.dynamicClient.Resource(edgeGVRMCP).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing edges in cluster %s: %w", p.cluster, err)
	}

	var targets []string
	for _, item := range list.Items {
		name := item.GetName()
		key := edgeConnKey(p.cluster, name)
		if _, ok := p.edgeConnManager.Load(key); ok {
			targets = append(targets, name)
		}
	}
	return targets, nil
}

// GetDerivedKubernetes returns a *mcpkubernetes.Kubernetes pointing at the edge
// agent's Kubernetes API, reachable via the hub's edges-proxy endpoint.
func (p *KedgeEdgeProvider) GetDerivedKubernetes(_ context.Context, edgeName string) (*mcpkubernetes.Kubernetes, error) {
	serverURL := fmt.Sprintf(
		"%s/clusters/%s/apis/kedge.faros.sh/v1alpha1/edges/%s/k8s",
		p.edgeProxyBase, p.cluster, edgeName,
	)

	restCfg := &rest.Config{
		Host:        serverURL,
		BearerToken: p.bearerToken,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	// Build a minimal in-memory kubeconfig so that NewKubernetes can
	// construct its clientcmd.ClientConfig.
	rawCfg := clientcmdapi.NewConfig()
	rawCfg.Clusters["edge"] = &clientcmdapi.Cluster{
		Server:                serverURL,
		InsecureSkipTLSVerify: true,
	}
	rawCfg.AuthInfos["user"] = &clientcmdapi.AuthInfo{
		Token: p.bearerToken,
	}
	rawCfg.Contexts["edge-ctx"] = &clientcmdapi.Context{
		Cluster:  "edge",
		AuthInfo: "user",
	}
	rawCfg.CurrentContext = "edge-ctx"

	clientCmdConfig := clientcmd.NewDefaultClientConfig(*rawCfg, nil)

	k8s, err := mcpkubernetes.NewKubernetes(minimalBaseConfig{}, clientCmdConfig, restCfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client for edge %s: %w", edgeName, err)
	}
	return k8s, nil
}

// GetDefaultTarget returns an empty string — there is no single default edge.
func (p *KedgeEdgeProvider) GetDefaultTarget() string { return "" }

// GetTargetParameterName returns "edge" as the MCP target query parameter.
func (p *KedgeEdgeProvider) GetTargetParameterName() string { return "edge" }

// WatchTargets is a no-op for v1; dynamic target reloading is not implemented.
func (p *KedgeEdgeProvider) WatchTargets(_ mcpkubernetes.McpReload) {}

// Close is a no-op.
func (p *KedgeEdgeProvider) Close() {}

// ─── minimalBaseConfig ───────────────────────────────────────────────────────
//
// minimalBaseConfig is the smallest possible implementation of mcpapi.BaseConfig
// needed to construct a mcpkubernetes.Kubernetes instance.  All feature flags
// default to their safe zero-values.

type minimalBaseConfig struct{}

var _ mcpapi.BaseConfig = minimalBaseConfig{}

func (minimalBaseConfig) IsRequireOAuth() bool               { return false }
func (minimalBaseConfig) GetClusterProviderStrategy() string { return mcpapi.ClusterProviderDisabled }
func (minimalBaseConfig) GetKubeConfigPath() string          { return "" }
func (minimalBaseConfig) GetDeniedResources() []mcpapi.GroupVersionKind {
	return nil
}
func (minimalBaseConfig) GetProviderConfig(_ string) (mcpapi.ExtendedConfig, bool) {
	return nil, false
}
func (minimalBaseConfig) GetToolsetConfig(_ string) (mcpapi.ExtendedConfig, bool) {
	return nil, false
}
func (minimalBaseConfig) GetStsClientId() string     { return "" } //nolint:staticcheck // interface name from upstream library
func (minimalBaseConfig) GetStsClientSecret() string { return "" }
func (minimalBaseConfig) GetStsAudience() string     { return "" }
func (minimalBaseConfig) GetStsScopes() []string     { return nil }
func (minimalBaseConfig) IsValidationEnabled() bool  { return false }
