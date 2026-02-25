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
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/faroshq/faros-kedge/pkg/util/connman"
)

// SiteRouteMap maps URL route keys to connManager tunnel keys.
// Route key format: "{clusterName}:{siteName}" (used in URL path).
// Tunnel key format: "{clusterName}/{siteName}" (used by connManager).
type SiteRouteMap struct {
	routes sync.Map
}

// NewSiteRouteMap creates a new SiteRouteMap.
func NewSiteRouteMap() *SiteRouteMap { return &SiteRouteMap{} }

// Set registers a mapping from route key to tunnel key.
func (m *SiteRouteMap) Set(routeKey, tunnelKey string) {
	m.routes.Store(routeKey, tunnelKey)
}

// Get looks up the tunnel key for a route key.
func (m *SiteRouteMap) Get(routeKey string) (string, bool) {
	v, ok := m.routes.Load(routeKey)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// virtualWorkspaces holds state and dependencies for all virtual workspaces.
type virtualWorkspaces struct {
	rootPathPrefix  string
	connManager     *connman.ConnectionManager
	edgeConnManager *ConnManager        // shared between agent-proxy-v2 and edges-proxy builders
	kcpConfig       *rest.Config        // kcp rest config for token verification (nil if kcp not configured)
	staticTokens    map[string]struct{} // static tokens that bypass JWT SA requirement
	siteRoutes      *SiteRouteMap
	logger          klog.Logger
}

// VirtualWorkspaceHandlers provides access to the HTTP handlers for tunneling.
type VirtualWorkspaceHandlers struct {
	vws *virtualWorkspaces
}

// NewVirtualWorkspaces creates a new VirtualWorkspaceHandlers.
// kcpConfig is required for SA token authorization against kcp. A nil
// kcpConfig causes the site-proxy handler to reject all requests with 503.
func NewVirtualWorkspaces(cm *connman.ConnectionManager, kcpConfig *rest.Config, siteRoutes *SiteRouteMap, staticTokens []string, logger klog.Logger) *VirtualWorkspaceHandlers {
	staticTokenSet := make(map[string]struct{}, len(staticTokens))
	for _, t := range staticTokens {
		staticTokenSet[t] = struct{}{}
	}
	return &VirtualWorkspaceHandlers{
		vws: &virtualWorkspaces{
			connManager:     cm,
			edgeConnManager: NewConnManager(),
			kcpConfig:       kcpConfig,
			siteRoutes:      siteRoutes,
			staticTokens:    staticTokenSet,
			logger:          logger.WithName("virtual-workspaces"),
		},
	}
}

// EdgeProxyHandler returns the HTTP handler for agent tunnel registration.
func (h *VirtualWorkspaceHandlers) EdgeProxyHandler() http.Handler {
	return h.vws.buildEdgeProxyHandler()
}

// AgentProxyHandler returns the HTTP handler for accessing agent resources.
func (h *VirtualWorkspaceHandlers) AgentProxyHandler() http.Handler {
	return h.vws.buildAgentProxyHandler()
}

// SiteProxyHandler returns the handler for proxying kube API to sites via reverse tunnels.
func (h *VirtualWorkspaceHandlers) SiteProxyHandler() http.Handler {
	return h.vws.buildSiteProxyHandler()
}

// EdgeAgentProxyHandler returns the handler for Edge agent tunnel registration.
// Agents connect to register their revdial tunnel for an Edge resource.
// Mount at /services/agent-proxy/.
func (h *VirtualWorkspaceHandlers) EdgeAgentProxyHandler() http.Handler {
	return h.vws.buildEdgeAgentProxyHandler()
}

// EdgesProxyHandler returns the handler for user-facing access to Edge resources.
// Supports the k8s (kubernetes API proxy) and ssh (WebSocket terminal) subresources.
// Mount at /services/edges-proxy/.
func (h *VirtualWorkspaceHandlers) EdgesProxyHandler() http.Handler {
	return h.vws.buildEdgesProxyHandler()
}
