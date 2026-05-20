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

// Package core registers the LinuxMCP "core" toolset (run_command and the
// minimal filesystem inspection tools).  Importing this package for side
// effects is sufficient to make the toolset available — the LinuxMCP server
// looks it up by name from the registry in pkg/linuxmcp.
package core

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/faroshq/faros-kedge/pkg/linuxmcp"
)

func init() {
	linuxmcp.Register("core", register)
}

// register is invoked by the LinuxMCP server when "core" is enabled.
//
// Tools are split into read-only and mutating groups; the latter are gated
// on the LinuxMCP spec.readOnly flag.
func register(srv *mcp.Server, p *linuxmcp.Provider) {
	// Read-only tools — always available.
	mcp.AddTool(srv, &mcp.Tool{
		Name: "run_command",
		Description: "Run a non-interactive shell command on a target Linux edge over SSH. " +
			"Returns stdout, stderr, exit code, and whether output was truncated. " +
			"Subject to spec.commandTimeoutSeconds and spec.maxOutputBytes on the LinuxMCP CR.",
	}, runCommandHandler(p))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_file",
		Description: "Read a file from a target Linux edge. Returns base64-encoded content for binary safety.",
	}, readFileHandler(p))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_dir",
		Description: "List the contents of a directory on a target Linux edge (ls -la style).",
	}, listDirHandler(p))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "stat_path",
		Description: "Return type / size / permissions / mtime for a path on a target Linux edge.",
	}, statPathHandler(p))

	// Mutating tools — skipped entirely when readOnly is set, so they don't
	// appear in tools/list and AI clients won't even try to call them.
	if !p.ReadOnly() {
		mcp.AddTool(srv, &mcp.Tool{
			Name: "write_file",
			Description: "Write content to a file on a target Linux edge. " +
				"Content is base64-encoded for binary safety. Disabled when LinuxMCP.spec.readOnly=true.",
		}, writeFileHandler(p))
	}
}

// ─── run_command ─────────────────────────────────────────────────────────────

// RunCommandInput is the JSON-schema-driven input for run_command.
type RunCommandInput struct {
	Command string `json:"command" jsonschema:"shell command to execute on the target edge"`
	Target  string `json:"target,omitempty" jsonschema:"edge name (defaults to the first connected edge in this LinuxMCP set)"`
}

// RunCommandOutput is the structured result of a run_command call.
type RunCommandOutput struct {
	Target     string `json:"target"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

func runCommandHandler(p *linuxmcp.Provider) mcp.ToolHandlerFor[RunCommandInput, RunCommandOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in RunCommandInput) (*mcp.CallToolResult, RunCommandOutput, error) {
		if in.Command == "" {
			return nil, RunCommandOutput{}, fmt.Errorf("run_command: \"command\" is required")
		}
		res, err := execShell(ctx, p, in.Target, in.Command)
		if err != nil {
			return nil, RunCommandOutput{}, fmt.Errorf("run_command: %w", err)
		}
		return nil, RunCommandOutput{
			Target:     res.Target,
			ExitCode:   res.ExitCode,
			Stdout:     res.Stdout,
			Stderr:     res.Stderr,
			Truncated:  res.Truncated,
			DurationMs: res.DurationMs,
		}, nil
	}
}
