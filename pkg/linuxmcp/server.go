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

package linuxmcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// implName is the MCP "Implementation" advertised to clients.
var implName = &mcp.Implementation{
	Name:    "kedge-linux-mcp",
	Version: "v0.1.0",
}

// Handler builds an http.Handler that serves the LinuxMCP streamable-HTTP
// endpoint.  One Provider == one request; the SDK's StreamableHTTPHandler
// invokes the getServer callback per request.
//
// Toolsets are filtered by `enabled` (matching LinuxMCPSpec.Toolsets); an
// empty `enabled` slice enables the default set (currently just "core").
func Handler(p *Provider, enabled []string) http.Handler {
	if len(enabled) == 0 {
		enabled = []string{"core"}
	}
	return mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server {
			return newServer(p, enabled)
		},
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

// newServer constructs a fresh mcp.Server and registers the requested
// toolsets against it.  Returning a fresh server per request matches the
// stateless model used by kube-mcp.
func newServer(p *Provider, enabled []string) *mcp.Server {
	srv := mcp.NewServer(implName, nil)
	for _, name := range enabled {
		if reg, ok := registry[name]; ok {
			reg(srv, p)
		}
	}
	return srv
}

// ToolsetRegistrar wires a named bundle of tools onto an mcp.Server.
// Toolsets register themselves in init() via Register().
type ToolsetRegistrar func(srv *mcp.Server, p *Provider)

var registry = map[string]ToolsetRegistrar{}

// Register adds a toolset under `name`.  Intended to be called from init().
func Register(name string, fn ToolsetRegistrar) {
	registry[name] = fn
}

// ToolsetNames returns the registered toolset names (for validation /
// discovery, e.g. in the controller).
func ToolsetNames() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
