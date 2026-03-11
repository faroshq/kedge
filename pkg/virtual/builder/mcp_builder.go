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
	"fmt"
	"net/http"
	"strings"

	mcpconfig "github.com/containers/kubernetes-mcp-server/pkg/config"
	mcpserver "github.com/containers/kubernetes-mcp-server/pkg/mcp"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// buildMCPHandler creates the HTTP handler for the per-tenant MCP endpoint.
//
// URL pattern (relative to /services/mcp/ mount point):
//
//	{cluster}/mcp      — StreamableHTTP (modern MCP clients)
//	{cluster}/sse      — SSE endpoint (legacy MCP clients)
//	{cluster}/message  — SSE message endpoint (legacy MCP clients)
//
// On each request the handler:
//  1. Extracts {cluster} from the URL path.
//  2. Extracts the caller's bearer token from the Authorization header.
//  3. Builds a KedgeEdgeProvider scoped to that cluster + token.
//  4. Spins up a fresh MCP server (stateless — no per-connection caching).
//  5. Delegates to ServeSse or ServeHTTP based on the path suffix.
func (p *virtualWorkspaces) buildMCPHandler(dynamicClient dynamic.Interface, edgeProxyBase string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := klog.FromContext(r.Context()).WithName("mcp-handler")
		// 1. Parse cluster name from the path that was left after stripping
		//    the "/services/mcp/" prefix.  Expected formats:
		//      "{cluster}/mcp"
		//      "{cluster}/sse"
		//      "{cluster}/message"
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			http.Error(w, "missing cluster path", http.StatusBadRequest)
			return
		}

		// Split off the last path segment as the endpoint kind.
		slashIdx := strings.LastIndex(path, "/")
		if slashIdx < 0 {
			http.Error(w, "invalid path: expected {cluster}/{endpoint}", http.StatusBadRequest)
			return
		}
		cluster := path[:slashIdx]
		endpoint := path[slashIdx+1:]

		if cluster == "" {
			http.Error(w, "empty cluster in path", http.StatusBadRequest)
			return
		}

		// 2. Extract bearer token.
		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 3. Build per-request MCP provider.
		//    dynamicClient is the hub's admin dynamic client; the per-edge REST
		//    configs built inside GetDerivedKubernetes use the caller's token.
		//    We need a cluster-scoped dynamic client to list Edges in that tenant.
		clusterDynamic, err := clusterScopedDynamicClient(p.kcpConfig, cluster)
		if err != nil {
			logger.Error(err, "failed to create cluster-scoped dynamic client", "cluster", cluster)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		provider := &KedgeEdgeProvider{
			cluster:         cluster,
			edgeConnManager: p.edgeConnManager,
			dynamicClient:   clusterDynamic,
			edgeProxyBase:   edgeProxyBase,
			bearerToken:     token,
		}

		// 4. Create a stateless MCP server for this request.
		staticCfg := &mcpconfig.StaticConfig{
			Stateless: true,
		}
		srv, err := mcpserver.NewServer(mcpserver.Configuration{StaticConfig: staticCfg}, provider)
		if err != nil {
			logger.Error(err, "failed to create MCP server", "cluster", cluster)
			http.Error(w, fmt.Sprintf("failed to initialize MCP server: %v", err), http.StatusInternalServerError)
			return
		}
		defer srv.Close()

		// 5. Dispatch to the appropriate MCP transport.
		switch endpoint {
		case "sse", "message":
			srv.ServeSse().ServeHTTP(w, r)
		case "mcp":
			srv.ServeHTTP().ServeHTTP(w, r)
		default:
			http.Error(w, fmt.Sprintf("unknown MCP endpoint %q; use /mcp or /sse", endpoint), http.StatusNotFound)
		}
	})
}

// clusterScopedDynamicClient creates a dynamic client whose Host is scoped to
// the given kcp cluster path.  It re-uses the hub's kcpConfig (admin creds) so
// that the hub can list Edge objects on behalf of any tenant.
func clusterScopedDynamicClient(kcpConfig *rest.Config, cluster string) (dynamic.Interface, error) {
	if kcpConfig == nil {
		return nil, fmt.Errorf("kcpConfig is nil; cannot create cluster-scoped client for cluster %s", cluster)
	}
	cfg := rest.CopyConfig(kcpConfig)
	cfg.Host = appendClusterPath(cfg.Host, cluster)
	return dynamic.NewForConfig(cfg)
}

// MCPHandler returns the HTTP handler for the per-tenant MCP endpoint.
// Mount at /services/mcp/.
func (h *VirtualWorkspaceHandlers) MCPHandler(dynamicClient dynamic.Interface, edgeProxyBase string) http.Handler {
	return h.vws.buildMCPHandler(dynamicClient, edgeProxyBase)
}
