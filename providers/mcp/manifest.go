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

// Package mcp is the first-party MCP aggregator provider. Bootstraps a
// CatalogEntry into root:kedge:providers via init(); the underlying
// KubernetesMCP/LinuxMCP/MCPServer controllers live in
// pkg/hub/controllers/mcp, pkg/hub/controllers/linuxmcp,
// pkg/hub/controllers/mcpserver respectively, and the aggregator runtime
// is in pkg/aggregatemcp.
//
// Imported for its side effect (RegisterBuiltin) from
// cmd/kedge-hub/main.go.
package mcp

import "github.com/faroshq/faros-kedge/pkg/hub/providers"

func init() {
	providers.RegisterBuiltin(providers.BuiltinSpec{
		Name:         "mcp",
		DisplayName:  "MCP",
		Description:  "Aggregated Model Context Protocol endpoints exposed by connected edges.",
		Category:     "AI",
		BuiltinRoute: "mcp",
		// mcp aggregates the per-edge-type MCP feeds shipped by both
		// kubernetes-edges and server-edges. An aggregator with no source
		// providers is empty by construction, so we hard-require both.
		Requires: []string{"kubernetes-edges", "server-edges"},
	})
}
