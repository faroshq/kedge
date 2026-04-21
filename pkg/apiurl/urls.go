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

// Package apiurl is the single source of truth for all kedge service path
// construction and URL parsing. All packages that build or decompose kedge
// hub URLs should use the helpers here instead of hand-crafting strings.
package apiurl

import (
	"fmt"
	"net/url"
	"strings"
)

// Path prefix constants for kedge virtual-workspace services and auth endpoints.
// All API paths live under /apis/ to separate them from the portal SPA served at /.
const (
	PathPrefixAgentProxy = "/apis/services/agent-proxy"
	PathPrefixEdgesProxy = "/apis/services/edges-proxy"
	PathPrefixMCP        = "/apis/services/mcp"
	PathAuthAuthorize    = "/apis/auth/authorize"
	PathAuthCallback     = "/apis/auth/callback"
	PathAuthRefresh      = "/apis/auth/refresh"
	PathAuthTokenLogin   = "/apis/auth/token-login"
	PathHealthz          = "/healthz"
)

// SplitBaseAndCluster splits a hub or kcp URL that contains a cluster path
// into a base URL (scheme+host only, no trailing slash) and the kcp cluster name.
//
// Accepts both external hub URLs (/apis/clusters/<name>) and internal kcp URLs
// (/clusters/<name>).
//
// Examples:
//
//	"https://hub:9443/apis/clusters/abc123"       → ("https://hub:9443", "abc123")
//	"https://hub:9443/apis/clusters/abc123/extra" → ("https://hub:9443", "abc123")
//	"https://kcp:6443/clusters/abc123"            → ("https://kcp:6443", "abc123")
//	"https://hub:9443"                            → ("https://hub:9443", "default")
//	"https://hub:9443/"                           → ("https://hub:9443", "default")
//
// Returns (trimmed url, "default") on parse error.
func SplitBaseAndCluster(rawURL string) (base, cluster string) {
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return strings.TrimRight(rawURL, "/"), "default"
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 5)
	// Match /apis/clusters/<name>[/...]
	if len(parts) >= 3 && parts[0] == "apis" && parts[1] == "clusters" && parts[2] != "" {
		u.Path = ""
		u.RawPath = ""
		return strings.TrimRight(u.String(), "/"), parts[2]
	}
	// Match /clusters/<name>[/...] (internal kcp URLs)
	if len(parts) >= 2 && parts[0] == "clusters" && parts[1] != "" {
		u.Path = ""
		u.RawPath = ""
		return strings.TrimRight(u.String(), "/"), parts[1]
	}
	u.Path = ""
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), "default"
}

// HubServerURL returns the hub's external URL with a /apis/clusters/<cluster> suffix,
// suitable for use in user-facing kubeconfigs and auth redirect URLs.
// Requests to this URL go through the hub's HTTP router (which strips /apis).
//
// For internal kcp connections (controllers, virtual workspaces) that bypass
// the hub router, use KCPClusterURL instead.
//
// If hubBase already contains a /apis/clusters/ or /clusters/ path it is replaced.
//
// Example: HubServerURL("https://hub:9443", "abc123") → "https://hub:9443/apis/clusters/abc123"
func HubServerURL(hubBase, cluster string) string {
	base := stripClusterPath(hubBase)
	return base + "/apis/clusters/" + cluster
}

// KCPClusterURL returns a kcp server URL with a /clusters/<cluster> suffix,
// suitable for direct kcp API connections (controllers, virtual workspaces,
// multicluster providers). These connections go directly to kcp and do NOT
// pass through the hub router, so the /apis prefix must be omitted.
//
// Example: KCPClusterURL("https://localhost:6443", "root:kedge:providers") → "https://localhost:6443/clusters/root:kedge:providers"
func KCPClusterURL(kcpBase, cluster string) string {
	base := stripClusterPath(kcpBase)
	return base + "/clusters/" + cluster
}

// stripClusterPath removes any existing /apis/clusters/... or /clusters/... suffix
// from a URL, returning just the scheme+host portion.
func stripClusterPath(u string) string {
	base := strings.TrimSuffix(u, "/")
	if idx := strings.Index(base, "/apis/clusters/"); idx != -1 {
		return base[:idx]
	}
	if idx := strings.Index(base, "/clusters/"); idx != -1 {
		return base[:idx]
	}
	return base
}

// EdgeAgentProxyPath returns the URL path (relative to the hub base) for the
// agent-proxy virtual workspace endpoint.
//
// Pattern: /apis/services/agent-proxy/{cluster}/apis/kedge.faros.sh/v1alpha1/edges/{name}/{subresource}
func EdgeAgentProxyPath(cluster, edgeName, subresource string) string {
	return fmt.Sprintf("%s/%s/apis/kedge.faros.sh/v1alpha1/edges/%s/%s",
		PathPrefixAgentProxy, cluster, edgeName, subresource)
}

// EdgeAgentProxyURL returns the full agent-proxy URL for use when dialling the
// hub tunnel endpoint.
func EdgeAgentProxyURL(hubBase, cluster, edgeName, subresource string) string {
	return strings.TrimRight(hubBase, "/") + EdgeAgentProxyPath(cluster, edgeName, subresource)
}

// EdgeProxyPath returns the URL path (relative to the hub base) for the
// edges-proxy virtual workspace endpoint.
//
// Pattern: /apis/services/edges-proxy/clusters/{cluster}/apis/kedge.faros.sh/v1alpha1/edges/{name}/{subresource}
func EdgeProxyPath(cluster, edgeName, subresource string) string {
	return fmt.Sprintf("%s/clusters/%s/apis/kedge.faros.sh/v1alpha1/edges/%s/%s",
		PathPrefixEdgesProxy, cluster, edgeName, subresource)
}

// EdgeProxyURL returns the full edges-proxy URL, combining the hub base URL
// with the EdgeProxyPath.
func EdgeProxyURL(hubBase, cluster, edgeName, subresource string) string {
	return strings.TrimRight(hubBase, "/") + EdgeProxyPath(cluster, edgeName, subresource)
}

// KubernetesMCPPath returns the URL path for the MCP virtual workspace endpoint.
//
// Pattern: /apis/services/mcp/{cluster}/apis/mcp.kedge.faros.sh/v1alpha1/kubernetes/{name}/mcp
func KubernetesMCPPath(cluster, kubernetesName string) string {
	return fmt.Sprintf("%s/%s/apis/mcp.kedge.faros.sh/v1alpha1/kubernetes/%s/mcp",
		PathPrefixMCP, cluster, kubernetesName)
}

// KubernetesMCPURL returns the full MCP endpoint URL.
func KubernetesMCPURL(hubBase, cluster, kubernetesName string) string {
	return strings.TrimRight(hubBase, "/") + KubernetesMCPPath(cluster, kubernetesName)
}

// EdgeAPIPath returns the kcp API path for an Edge resource, suitable for use
// as a client Host suffix or in kubeconfig server URLs.
//
// Pattern: /apis/clusters/{cluster}/apis/kedge.faros.sh/v1alpha1/edges/{name}
func EdgeAPIPath(cluster, edgeName string) string {
	return fmt.Sprintf("/apis/clusters/%s/apis/kedge.faros.sh/v1alpha1/edges/%s", cluster, edgeName)
}

// ExternalizeURL replaces the scheme and host in edgeURL with those from
// hubBase, making an internal edge-proxy URL routable through the public hub.
//
// If edgeURL does not start with /apis/services/ it is returned unchanged (it is
// already an absolute URL with the correct host, or is not a services path).
func ExternalizeURL(edgeURL, hubBase string) (string, error) {
	if !strings.HasPrefix(edgeURL, "/apis/services/") {
		return edgeURL, nil
	}
	hub, err := url.Parse(strings.TrimRight(hubBase, "/"))
	if err != nil {
		return "", fmt.Errorf("parsing hub base URL %q: %w", hubBase, err)
	}
	result := hub.Scheme + "://" + hub.Host + edgeURL
	return result, nil
}
