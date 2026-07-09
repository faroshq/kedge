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

package api

// Additional App Studio MCP surface: a prompt that ships the git-native
// workflow + domain rules so external clients auto-discover them (no manual
// skill install), read-only resources for project discovery, a deployment
// handoff tool, and a no-clone write path (write_file + commit_files) for
// agents that author file contents directly instead of pushing a local clone.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/faroshq/provider-app-studio/workspace"
)

// projectAssistantMCPWorkflowPrompt is the git-native workflow guidance plus the
// domain rules distilled from the App Studio system prompt. Serving it as an MCP
// prompt means any connected client (Claude Code, Cursor) surfaces it without
// the user copying a SKILL.md into their harness.
const projectAssistantMCPWorkflowPrompt = `You are working on a kedge App Studio project: a chat-built application backed by a GitHub repository and a live development sandbox that serves a preview.

Core loop (git-native — kedge hosts no git server, repos live on GitHub):
1. Find the project: list_projects, then get_project (returns the GitHub cloneURL).
2. Clone that URL locally with the user's GitHub credentials and edit with your own tools; commit and push to GitHub. (If you are not cloning, use write_file then commit_files instead.)
3. Call sync_workspace_from_repo to pull the pushed commit into the sandbox and rebuild the preview. The sync is asynchronous.
4. Call verify_project. If it returns failing, read the errors (or get_runtime_logs), fix, push/sync, and verify again. Cap this at ~3 attempts, then stop and report the remaining error rather than looping.
5. get_preview_url returns the live URL once the sandbox is serving.

Domain rules:
- The bound development template is the app's environment contract. Back-end services it declares (e.g. a managed database with an injected DATABASE_URL) already exist for the sandbox — do not provision a duplicate, and do not conclude a service is missing just because the code does not use it yet.
- Provision supporting infrastructure only when the user explicitly asks and the current sandbox cannot satisfy the need.
- Separate development from production; a production launch is a distinct step.
- Do not invent platform capabilities; if you cannot verify one, say so.
- Tenant identity comes from your bearer token; never ask the user for a workspace path.`

func (s *Server) registerMCPPrompts(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "kedge_app_studio_workflow",
		Title:       "kedge App Studio: build & ship workflow",
		Description: "How to build, sync, verify, and preview a kedge App Studio app from your own harness, plus the platform's domain rules.",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "kedge App Studio git-native workflow and domain rules",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: projectAssistantMCPWorkflowPrompt},
				},
			},
		}, nil
	})
}

func (s *Server) registerMCPResources(srv *mcp.Server, id identity) {
	// A single read-only resource listing the caller's projects, so a client can
	// browse available projects without issuing a tool call.
	srv.AddResource(&mcp.Resource{
		Name:        "projects",
		URI:         "appstudio://projects",
		Title:       "App Studio projects",
		Description: "JSON list of the App Studio projects in your workspace (name, template, repository).",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		if strings.TrimSpace(id.tenantPath) == "" {
			return nil, fmt.Errorf("no tenant context on this request")
		}
		c, err := s.clientFor(id)
		if err != nil {
			return nil, fmt.Errorf("build project client: %w", err)
		}
		list, err := c.Projects().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		out := mcpListProjectsOutput{Projects: make([]mcpProjectSummary, 0, len(list.Items))}
		for i := range list.Items {
			out.Projects = append(out.Projects, mcpProjectSummaryOf(&list.Items[i]))
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("encode projects: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "application/json", Text: string(raw)},
			},
		}, nil
	})
}

// ---- write path (no-clone) + deploy ----

type mcpWriteFileInput struct {
	Project string `json:"project" jsonschema:"the App Studio project name"`
	Path    string `json:"path" jsonschema:"project-relative file path"`
	Content string `json:"content" jsonschema:"complete UTF-8 file content to write"`
}

type mcpWriteFileOutput struct {
	Path    string `json:"path"`
	Bytes   int    `json:"bytes"`
	Synced  bool   `json:"synced"`
	Message string `json:"message,omitempty"`
}

type mcpCommitFilesInput struct {
	Project string   `json:"project" jsonschema:"the App Studio project name"`
	Paths   []string `json:"paths" jsonschema:"project-relative paths to commit (must already be written to the workspace)"`
	Message string   `json:"message,omitempty" jsonschema:"commit message"`
	Branch  string   `json:"branch,omitempty" jsonschema:"target branch; defaults to the repository default"`
}

type mcpCommitFilesOutput struct {
	// Result is the Code provider's commit response (commit SHA, request name,
	// phase, branch, files). Raw is the JSON payload as-is when it does not
	// decode into an object.
	Result map[string]any `json:"result,omitempty"`
	Raw    string         `json:"raw,omitempty"`
}

type mcpDeployInput struct {
	Project   string `json:"project" jsonschema:"the App Studio project name"`
	TargetRef string `json:"targetRef" jsonschema:"RuntimeTarget name or reference that should run this app"`
	Image     string `json:"image,omitempty" jsonschema:"OCI image to deploy"`
	Port      int64  `json:"port,omitempty" jsonschema:"container port exposed by the app"`
	Intent    string `json:"intent,omitempty" jsonschema:"deployment intent: preview or production"`
}

func (s *Server) registerMCPWriteTools(srv *mcp.Server, r *http.Request, id identity) {
	yes := true

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "write_file",
		Title:       "Write a file into the App Studio workspace",
		Description: "Write a complete UTF-8 file into the project's development workspace and trigger a sandbox sync. Use this for the no-clone path (authoring contents directly); for local edits, push to git and call sync_workspace_from_repo instead. Follow up with commit_files to persist to the repository.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: &yes},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpWriteFileInput) (*mcp.CallToolResult, mcpWriteFileOutput, error) {
		_, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, mcpWriteFileOutput{}, err
		}
		if _, err := s.workspaces.WriteFile(ctx, projectWorkspaceScope(id, p.Name), workspace.WriteOptions{Path: in.Path, Content: in.Content}); err != nil {
			return nil, mcpWriteFileOutput{}, fmt.Errorf("write file: %w", err)
		}
		// Push the change into the live development sandbox, mirroring the
		// assistant's post-mutation sync.
		s.syncDevelopmentAfterMutation(id, p.DeepCopy(), projectToolWriteFile)
		return nil, mcpWriteFileOutput{
			Path:    in.Path,
			Bytes:   len([]byte(in.Content)),
			Synced:  true,
			Message: "written and sandbox sync triggered (asynchronous); verify_project to confirm",
		}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "commit_files",
		Title:       "Commit workspace files to the git repository",
		Description: "Commit the named workspace files (already written via write_file or synced) to the project's GitHub repository. Creates a visible commit request through the Code provider.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: &yes},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCommitFilesInput) (*mcp.CallToolResult, mcpCommitFilesOutput, error) {
		_, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, mcpCommitFilesOutput{}, err
		}
		ref := projectLinkedRepositoryRef(p)
		if ref == "" {
			return nil, mcpCommitFilesOutput{}, fmt.Errorf("project %q has no linked repository", p.Name)
		}
		args := map[string]any{
			"repositoryRef": ref,
			"paths":         toAnySlice(in.Paths),
		}
		if strings.TrimSpace(in.Message) != "" {
			args["message"] = in.Message
		}
		if strings.TrimSpace(in.Branch) != "" {
			args["branch"] = in.Branch
		}
		result, err := s.commitProjectWorkspaceFiles(ctx, id, projectWorkspaceScope(id, p.Name), ref, s.mcpEndpoint(id.clusterID), r, args)
		if err != nil {
			return nil, mcpCommitFilesOutput{}, fmt.Errorf("commit files: %w", err)
		}
		out := mcpCommitFilesOutput{}
		if err := json.Unmarshal([]byte(result), &out.Result); err != nil || out.Result == nil {
			out.Result = nil
			out.Raw = result
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "deploy",
		Title:       "Prepare a production deployment handoff",
		Description: "Create a deterministic deployment handoff for this project from an OCI image, runtime target, and port. Returns structured blockers until a tenant RuntimeTarget is configured — it does not silently succeed.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: &yes},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpDeployInput) (*mcp.CallToolResult, projectAssistantRuntimeWorkflowResult, error) {
		c, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, projectAssistantRuntimeWorkflowResult{}, err
		}
		runCtx := s.mcpRunContext(id, c, p)
		normalized, err := projectAssistantRuntimeWorkflowInputFromDeployTool(runCtx)(ctx, &projectAssistantRuntimeDeployToolInput{
			TargetRef: in.TargetRef,
			Image:     in.Image,
			Port:      in.Port,
			Intent:    in.Intent,
		})
		if err != nil {
			return nil, projectAssistantRuntimeWorkflowResult{}, err
		}
		res, err := formatProjectAssistantRuntimeDeploymentResult(ctx, normalized)
		if err != nil {
			return nil, projectAssistantRuntimeWorkflowResult{}, err
		}
		return nil, *res, nil
	})
}

func toAnySlice(in []string) []any {
	out := make([]any, 0, len(in))
	for _, v := range in {
		out = append(out, v)
	}
	return out
}
