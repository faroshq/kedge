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

// Package virtual builds the LinuxMCP virtual-workspace HTTP handler for
// the server-edges first-party provider. Registered with the hub via
// BuiltinSpec.VirtualWorkspaceHandler in the sibling manifest.go.
//
// On each request:
//  1. Parse /{cluster}/apis/kedge.faros.sh/v1alpha1/linuxmcps/{name}/mcp
//  2. Read the LinuxMCP CR → edge selector + policy knobs.
//  3. List Edges, keep server-type ones matching the selector with a live
//     tunnel.
//  4. Build a linuxmcp.Provider with an OpenSession callback driven by
//     deps.OpenSSHSession (framework provides the SSH dance).
//  5. Hand off to the linuxmcp SDK's streamable HTTP handler.
package virtual

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	"github.com/faroshq/faros-kedge/pkg/apiurl"
	"github.com/faroshq/faros-kedge/pkg/virtual/builder"
	"github.com/faroshq/faros-kedge/providers/serveredges/linuxmcp"

	// Side-effect imports: register LinuxMCP toolsets so they appear in
	// the in-process registry by name.
	_ "github.com/faroshq/faros-kedge/providers/serveredges/linuxmcp/toolsets/core"
	_ "github.com/faroshq/faros-kedge/providers/serveredges/linuxmcp/toolsets/diag"
	_ "github.com/faroshq/faros-kedge/providers/serveredges/linuxmcp/toolsets/net"
	_ "github.com/faroshq/faros-kedge/providers/serveredges/linuxmcp/toolsets/pkg"
	_ "github.com/faroshq/faros-kedge/providers/serveredges/linuxmcp/toolsets/systemd"
)

// linuxMCPGVR is the GVR for LinuxMCP CRs the handler resolves per request.
var linuxMCPGVR = schema.GroupVersionResource{
	Group:    "kedge.faros.sh",
	Version:  "v1alpha1",
	Resource: "linuxmcps",
}

// Build returns the http.Handler the hub mounts at /services/linux-mcp/.
func Build(deps *builder.Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := klog.FromContext(r.Context()).WithName("linux-mcp-handler")

		cluster, lmcpName, ok := parseLinuxMCPPath(r.URL.Path)
		if !ok {
			http.Error(w, "invalid path: expected /{cluster}/apis/kedge.faros.sh/v1alpha1/linuxmcps/{name}/mcp", http.StatusBadRequest)
			return
		}

		token := builder.ExtractBearerToken(r)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if deps.KCPConfig == nil {
			http.Error(w, "kcp not configured", http.StatusInternalServerError)
			return
		}
		dynClient, err := builder.ClusterScopedDynamicClient(deps.KCPConfig, cluster)
		if err != nil {
			logger.Error(err, "failed to build cluster-scoped dynamic client", "cluster", cluster)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		lmcpObj, err := dynClient.Resource(linuxMCPGVR).Get(ctx, lmcpName, metav1.GetOptions{})
		if err != nil {
			logger.Error(err, "failed to get LinuxMCP", "name", lmcpName)
			http.Error(w, fmt.Sprintf("LinuxMCP %q not found: %v", lmcpName, err), http.StatusNotFound)
			return
		}

		specRaw, _, _ := builder.UnstructuredNestedMap(lmcpObj.Object, "spec")

		// Resolve the edge selector.
		var edgeSelector labels.Selector
		edgeSelectorRaw, _, _ := builder.UnstructuredNestedMap(specRaw, "edgeSelector")
		if len(edgeSelectorRaw) > 0 {
			ls := &metav1.LabelSelector{}
			if matchLabels, ok := edgeSelectorRaw["matchLabels"].(map[string]any); ok {
				ls.MatchLabels = make(map[string]string)
				for k, v := range matchLabels {
					if s, ok := v.(string); ok {
						ls.MatchLabels[k] = s
					}
				}
			}
			edgeSelector, err = metav1.LabelSelectorAsSelector(ls)
			if err != nil {
				logger.Error(err, "invalid edgeSelector")
				http.Error(w, "invalid edgeSelector", http.StatusBadRequest)
				return
			}
		} else {
			edgeSelector = labels.Everything()
		}

		// Resolve target edges: server-type only, selector match, tunnel active.
		edgeList, err := dynClient.Resource(builder.EdgeGVRForMCPSelector).List(ctx, metav1.ListOptions{})
		if err != nil {
			logger.Error(err, "failed to list edges", "cluster", cluster)
			http.Error(w, "failed to list edges", http.StatusInternalServerError)
			return
		}

		var resolvedEdges []string
		for _, edgeObj := range edgeList.Items {
			edgeName := edgeObj.GetName()
			edgeType, _, _ := unstructured.NestedString(edgeObj.Object, "spec", "type")
			if edgeType != "server" {
				continue
			}
			if !edgeSelector.Matches(labels.Set(edgeObj.GetLabels())) {
				continue
			}
			key := builder.EdgeConnKey(cluster, edgeName)
			if _, ok := deps.EdgeConnManager.Load(key); ok {
				resolvedEdges = append(resolvedEdges, edgeName)
			}
		}
		logger.Info("LinuxMCP resolved edges", "cluster", cluster, "edges", resolvedEdges)

		// Pull policy knobs off the spec (defaults applied inside the package).
		var cmdTimeout time.Duration
		if v, ok := specRaw["commandTimeoutSeconds"].(int64); ok && v > 0 {
			cmdTimeout = time.Duration(v) * time.Second
		}
		var maxOut int
		if v, ok := specRaw["maxOutputBytes"].(int64); ok && v > 0 {
			maxOut = int(v)
		}
		readOnly, _, _ := unstructured.NestedBool(lmcpObj.Object, "spec", "readOnly")

		// Build the OpenSession callback that dials the agent tunnel and
		// opens an SSH client. Mirrors edgesSSHHandler in the framework's
		// edges_proxy_builder.go.
		callerIdentity := builder.ResolveCallerIdentity(ctx, deps.KCPConfig, token, deps.Logger)
		openSession := openLinuxSession(deps, cluster, callerIdentity)

		// Toolset list — default to ["core"] if empty.
		var toolsets []string
		if raw, ok := specRaw["toolsets"].([]any); ok {
			for _, t := range raw {
				if s, ok := t.(string); ok {
					toolsets = append(toolsets, s)
				}
			}
		}

		provider := linuxmcp.NewProvider(linuxmcp.Config{
			Cluster:        cluster,
			EdgeNames:      resolvedEdges,
			OpenSession:    openSession,
			CommandTimeout: cmdTimeout,
			MaxOutputBytes: maxOut,
			ReadOnly:       readOnly,
		})

		// Per-endpoint metadata: tells AI clients which kedge tenant +
		// LinuxMCP CR they're connected to so the model can reason about
		// scope before issuing any tool call. spec.displayName +
		// spec.instructions on the CR override the auto-generated values.
		userDisplayName, _, _ := unstructured.NestedString(lmcpObj.Object, "spec", "displayName")
		userInstructions, _, _ := unstructured.NestedString(lmcpObj.Object, "spec", "instructions")
		title := userDisplayName
		if title == "" {
			title = fmt.Sprintf("Kedge Linux — %s (tenant %s)", lmcpName, cluster)
		}
		instructions := userInstructions
		if instructions == "" {
			instructions = fmt.Sprintf(
				"You are connected to the kedge LinuxMCP endpoint %q in tenant workspace %q. "+
					"It runs shell-style tools over SSH against the server-type edges this CR's "+
					"edgeSelector matches.  Use the \"target\" argument on any tool to pick a specific "+
					"edge — omit it and the first connected edge is used.  For Kubernetes clusters, "+
					"use the KubernetesMCP endpoint instead; for one endpoint covering both with a "+
					"list_targets discovery tool, use the kedge aggregate MCPServer endpoint.",
				lmcpName, cluster,
			)
		}
		meta := linuxmcp.Meta{
			Name:         "kedge-linux-mcp-" + lmcpName,
			Title:        title,
			Instructions: instructions,
			About: linuxmcp.AboutDoc{
				Role:         "linux",
				Capabilities: []string{"linux", "ssh"},
				Tenant:       cluster,
				LinuxMCP:     lmcpName,
				EndpointURL:  deps.HubExternalURL + apiurl.LinuxMCPPath(cluster, lmcpName),
				Toolsets:     toolsets,
				ReadOnly:     readOnly,
			},
		}

		linuxmcp.Handler(provider, toolsets, meta).ServeHTTP(w, r)
	})
}

// openLinuxSession returns an OpenSession callback bound to one request's
// (cluster, callerIdentity). Defers the actual SSH dance to the
// framework's Deps.OpenSSHSession helper.
func openLinuxSession(deps *builder.Deps, cluster, callerIdentity string) linuxmcp.OpenSessionFunc {
	return func(ctx context.Context, edgeName string) (*gossh.Client, error) {
		logger := klog.FromContext(ctx).WithName("linux-mcp-open-session").
			WithValues("cluster", cluster, "edge", edgeName)
		return deps.OpenSSHSession(ctx, cluster, edgeName, callerIdentity, logger)
	}
}

// parseLinuxMCPPath extracts cluster + LinuxMCP name from the path seen
// by the /services/linux-mcp handler after StripPrefix.
//
// Expected format:
//
//	/{cluster}/apis/kedge.faros.sh/v1alpha1/linuxmcps/{name}/mcp
func parseLinuxMCPPath(path string) (cluster, name string, ok bool) {
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 8)
	if len(parts) < 7 {
		return "", "", false
	}
	if parts[1] != "apis" || parts[2] != "kedge.faros.sh" || parts[3] != "v1alpha1" ||
		parts[4] != "linuxmcps" || parts[6] != "mcp" {
		return "", "", false
	}
	return parts[0], parts[5], true
}
