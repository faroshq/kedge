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
	"k8s.io/klog/v2"
)

// buildMCPHandler creates the HTTP handler for the per-edge MCP endpoint.
//
// URL pattern (mounted via agent-proxy route):
//
//	/services/agent-proxy/{cluster}/apis/kedge.faros.sh/v1alpha1/edges/{edgeName}/mcp
//
// cluster and edgeName are extracted from the path by the agent-proxy mux before
// this handler is called; they are passed as explicit arguments rather than
// parsed from the request URL.
//
// On each request the handler:
//  1. Extracts the caller's bearer token from the Authorization header.
//  2. Builds a single-edge KedgeEdgeProvider for (cluster, edgeName).
//  3. Spins up a fresh stateless MCP server.
//  4. Serves via the streamable-HTTP transport (ServeHTTP).
func (p *virtualWorkspaces) buildMCPHandler(cluster, edgeName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := klog.FromContext(r.Context()).WithName("mcp-handler")

		// 1. Extract bearer token.
		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 2. Build per-request single-edge MCP provider.
		//    edgeProxyBase is derived from the hub's external URL.
		edgeProxyBase := strings.TrimRight(p.hubExternalURL, "/") + "/services/edges-proxy"

		provider := &KedgeEdgeProvider{
			cluster:         cluster,
			edgeName:        edgeName,
			edgeConnManager: p.edgeConnManager,
			edgeProxyBase:   edgeProxyBase,
			bearerToken:     token,
		}

		// 3. Create a stateless MCP server for this request.
		staticCfg := &mcpconfig.StaticConfig{
			Stateless: true,
		}
		srv, err := mcpserver.NewServer(mcpserver.Configuration{StaticConfig: staticCfg}, provider)
		if err != nil {
			logger.Error(err, "failed to create MCP server", "cluster", cluster, "edge", edgeName)
			http.Error(w, fmt.Sprintf("failed to initialize MCP server: %v", err), http.StatusInternalServerError)
			return
		}
		defer srv.Close()

		// 4. Serve via streamable HTTP transport.
		srv.ServeHTTP().ServeHTTP(w, r)
	})
}
