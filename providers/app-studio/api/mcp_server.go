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

// App Studio's MCP surface. This exposes App Studio's app-building capabilities
// as Model Context Protocol tools so an EXTERNAL harness — Claude Code, Cursor,
// Codex, any MCP client — can drive a kedge App Studio project directly, using
// its own agent loop and its own local editor.
//
// The intended workflow is git-native: the developer clones the project's
// GitHub repository locally (kedge hosts no git server; repos live on GitHub),
// edits with their harness's native tools, pushes to GitHub, then calls
// sync_workspace_from_repo to pull the pushed commit into the live development
// sandbox and rebuild the preview. verify_project / get_runtime_logs /
// get_preview_url close the edit -> error -> fix loop.
//
// This mirrors the infrastructure provider's mcpserver (providers/infrastructure
// /mcpserver): a per-request MCP server whose tool handlers close over the
// caller's tenant identity, taken from the hub-proxy-injected headers. It lives
// in the api package (not a separate module) so tool bodies reuse the existing
// project / workspace / runtime operations directly instead of re-exporting them.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	asclient "github.com/faroshq/provider-app-studio/client"
	"github.com/faroshq/provider-app-studio/workspace"
)

// MCPHandler returns the streamable-HTTP MCP handler App Studio mounts at /mcp.
// A fresh server is built per request (Stateless) so each caller's tenant
// identity is isolated inside the tool-handler closures.
func (s *Server) MCPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return s.newMCPServer(r)
		},
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

func (s *Server) newMCPServer(r *http.Request) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "kedge-app-studio",
		Version: "0.1.0",
		Title:   "kedge App Studio",
	}, &mcp.ServerOptions{
		Instructions: "This MCP endpoint drives a kedge App Studio project — a " +
			"chat-built application backed by a GitHub repository and a live " +
			"development sandbox. Typical loop: call list_projects to find the " +
			"project, clone its GitHub repository locally (get_project returns " +
			"the clone URL), edit and push with your own tools, then call " +
			"sync_workspace_from_repo to load the pushed commit into the sandbox " +
			"and rebuild the preview. Use verify_project to check the build, " +
			"get_runtime_logs to read errors, and get_preview_url for the live " +
			"URL. The bound development template is the app's environment " +
			"contract: back-end services it declares (e.g. a database) are " +
			"already injected — do not provision duplicates. Tenant identity is " +
			"taken from your bearer token; never ask the user for a workspace path.",
	})
	s.registerMCPTools(srv, r)
	return srv
}

// mcpIdentityFromRequest extracts the caller identity from the hub-proxy
// headers without writing an HTTP error (unlike identityFromRequest). The zero
// identity flows through to tool handlers, which surface a clear auth error.
func mcpIdentityFromRequest(r *http.Request) identity {
	tenantPath := strings.TrimSpace(r.Header.Get("X-Kedge-Tenant"))
	org, ws := parseTenantPath(tenantPath)
	return identity{
		tenantPath:    tenantPath,
		clusterID:     strings.TrimSpace(r.Header.Get("X-Kedge-Cluster")),
		orgUUID:       org,
		workspaceUUID: ws,
		user:          strings.TrimSpace(r.Header.Get("X-Kedge-User")),
		token:         bearerToken(r),
	}
}

// mcpProjectContext resolves the caller identity, project client, and the named
// Project. It is the common preamble for every project-scoped tool.
func (s *Server) mcpProjectContext(ctx context.Context, id identity, projectName string) (*asclient.Client, *aiv1alpha1.Project, error) {
	if strings.TrimSpace(id.tenantPath) == "" {
		return nil, nil, fmt.Errorf("no tenant context on this request; the hub did not resolve a workspace")
	}
	if strings.TrimSpace(projectName) == "" {
		return nil, nil, fmt.Errorf("project is required; call list_projects to discover available projects")
	}
	c, err := s.clientFor(id)
	if err != nil {
		return nil, nil, fmt.Errorf("build project client: %w", err)
	}
	p, err := c.Projects().Get(ctx, projectName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("get project %q: %w", projectName, err)
	}
	return c, p, nil
}

// mcpRunContext builds the workflow run context the runtime/verify tools consume.
func (s *Server) mcpRunContext(id identity, c *asclient.Client, p *aiv1alpha1.Project) projectAssistantWorkflowRunContext {
	return projectAssistantWorkflowRunContext{
		Server:         s,
		Project:        p,
		WorkspaceScope: projectWorkspaceScope(id, p.Name),
		RunState:       newProjectEinoAssistantRunState(),
		Identity:       id,
		Client:         c,
	}
}

// ---- tool input/output types ----

type mcpProjectInput struct {
	Project string `json:"project" jsonschema:"the App Studio project name (from list_projects)"`
}

type mcpProjectSummary struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName,omitempty"`
	Description   string `json:"description,omitempty"`
	Template      string `json:"template,omitempty"`
	RepositoryRef string `json:"repositoryRef,omitempty"`
	// HTMLURL and CloneURL are the project's GitHub repository, resolved from
	// the Repository resource. Populated by get_project (list_projects leaves
	// them empty to avoid a per-project resource read).
	HTMLURL  string `json:"htmlURL,omitempty"`
	CloneURL string `json:"cloneURL,omitempty"`
}

type mcpListProjectsOutput struct {
	Projects []mcpProjectSummary `json:"projects"`
}

type mcpListFilesInput struct {
	Project string `json:"project" jsonschema:"the App Studio project name"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of file paths to return"`
}

type mcpListFilesOutput struct {
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated,omitempty"`
}

type mcpReadFileInput struct {
	Project  string `json:"project" jsonschema:"the App Studio project name"`
	Path     string `json:"path" jsonschema:"project-relative file path"`
	MaxBytes int    `json:"maxBytes,omitempty" jsonschema:"maximum bytes to return"`
}

type mcpSearchFilesInput struct {
	Project    string `json:"project" jsonschema:"the App Studio project name"`
	Query      string `json:"query" jsonschema:"text to search for across workspace files"`
	MaxResults int    `json:"maxResults,omitempty" jsonschema:"maximum matching files to return"`
}

func (s *Server) registerMCPTools(srv *mcp.Server, r *http.Request) {
	id := mcpIdentityFromRequest(r)
	yes := true
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &yes}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Title:       "List App Studio projects",
		Description: "List the App Studio projects in your workspace with their bound template and GitHub repository. Call this first to find the project to work on.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpListProjectsOutput, error) {
		if strings.TrimSpace(id.tenantPath) == "" {
			return nil, mcpListProjectsOutput{}, fmt.Errorf("no tenant context on this request")
		}
		c, err := s.clientFor(id)
		if err != nil {
			return nil, mcpListProjectsOutput{}, fmt.Errorf("build project client: %w", err)
		}
		list, err := c.Projects().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, mcpListProjectsOutput{}, fmt.Errorf("list projects: %w", err)
		}
		out := mcpListProjectsOutput{Projects: make([]mcpProjectSummary, 0, len(list.Items))}
		for i := range list.Items {
			out.Projects = append(out.Projects, mcpProjectSummaryOf(&list.Items[i]))
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_project",
		Title:       "Get an App Studio project",
		Description: "Return a project's bound development template and its GitHub repository clone URL. Clone that URL locally to edit the app with your own tools, then push and call sync_workspace_from_repo.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectInput) (*mcp.CallToolResult, mcpProjectSummary, error) {
		c, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, mcpProjectSummary{}, err
		}
		out := mcpProjectSummaryOf(p)
		if view := projectRepositoryView(ctx, c, p); view != nil {
			out.HTMLURL = strings.TrimSpace(view.HTMLURL)
			if out.HTMLURL != "" {
				out.CloneURL = out.HTMLURL + ".git"
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_files",
		Title:       "List App Studio workspace files",
		Description: "List the files currently in the project's live development workspace (the tree synced to the sandbox). Use this to see server-side state; your local git clone is the edit surface.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpListFilesInput) (*mcp.CallToolResult, mcpListFilesOutput, error) {
		_, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, mcpListFilesOutput{}, err
		}
		files, err := s.workspaces.ListFiles(ctx, projectWorkspaceScope(id, p.Name), workspace.ListOptions{Limit: in.Limit})
		if err != nil {
			return nil, mcpListFilesOutput{}, fmt.Errorf("list files: %w", err)
		}
		out := mcpListFilesOutput{Files: make([]string, 0, len(files.Files)), Truncated: files.Truncated}
		for _, f := range files.Files {
			out.Files = append(out.Files, f.Path)
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_file",
		Title:       "Read an App Studio workspace file",
		Description: "Read a bounded UTF-8 text file from the project's live development workspace.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpReadFileInput) (*mcp.CallToolResult, workspace.FileContent, error) {
		_, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, workspace.FileContent{}, err
		}
		content, err := s.workspaces.ReadFile(ctx, projectWorkspaceScope(id, p.Name), workspace.ReadOptions{Path: in.Path, MaxBytes: in.MaxBytes})
		if err != nil {
			return nil, workspace.FileContent{}, fmt.Errorf("read file: %w", err)
		}
		return nil, content, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_files",
		Title:       "Search App Studio workspace files",
		Description: "Search text files in the project's live development workspace and return bounded path/fragment matches.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSearchFilesInput) (*mcp.CallToolResult, workspace.SearchResult, error) {
		_, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, workspace.SearchResult{}, err
		}
		res, err := s.workspaces.SearchFiles(ctx, projectWorkspaceScope(id, p.Name), workspace.SearchOptions{Query: in.Query, MaxResults: in.MaxResults})
		if err != nil {
			return nil, workspace.SearchResult{}, fmt.Errorf("search files: %w", err)
		}
		return nil, res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "sync_workspace_from_repo",
		Title:       "Sync the sandbox from the latest git commit",
		Description: "Pull the latest commit from the project's GitHub repository into the live development workspace and push it into the sandbox, rebuilding the preview. Call this after you push local edits. The sandbox sync runs asynchronously — poll verify_project or get_runtime_logs afterwards to confirm the build.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: &yes},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectInput) (*mcp.CallToolResult, projectHydrateResponse, error) {
		_, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, projectHydrateResponse{}, err
		}
		ref := projectLinkedRepositoryRef(p)
		if ref == "" {
			return nil, projectHydrateResponse{}, fmt.Errorf("project %q has no linked repository to sync from", p.Name)
		}
		resp, err := s.hydrateWorkspaceFromRepository(ctx, id, p, r, ref)
		if err != nil {
			return nil, projectHydrateResponse{}, fmt.Errorf("sync workspace from repository: %w", err)
		}
		return nil, resp, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "verify_project",
		Title:       "Verify the app builds and serves",
		Description: "Inspect the live development runtime and its recent logs for build, compile, or crash errors, and report whether the app is serving cleanly or failing. Call this after sync_workspace_from_repo to confirm a change works.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectInput) (*mcp.CallToolResult, projectAssistantVerifyResult, error) {
		c, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, projectAssistantVerifyResult{}, err
		}
		res, err := verifyProjectAssistantRuntime(s.mcpRunContext(id, c, p))(ctx, &projectAssistantVerifyToolInput{})
		if err != nil {
			return nil, projectAssistantVerifyResult{}, err
		}
		return nil, *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_runtime_logs",
		Title:       "Read development runtime logs",
		Description: "Return recent development runtime logs from the live sandbox so you can diagnose why the app is not building or serving.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectInput) (*mcp.CallToolResult, projectAssistantRuntimeLogsResult, error) {
		c, p, err := s.mcpProjectContext(ctx, id, in.Project)
		if err != nil {
			return nil, projectAssistantRuntimeLogsResult{}, err
		}
		res, err := fetchProjectAssistantRuntimeLogs(s.mcpRunContext(id, c, p))(ctx, &projectAssistantRuntimeLogsToolInput{})
		if err != nil {
			return nil, projectAssistantRuntimeLogsResult{}, err
		}
		return nil, *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_runtime_status",
		Title:       "Get development runtime status",
		Description: "Return whether the development sandbox is provisioning, starting, serving preview traffic, or not deployed.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectInput) (*mcp.CallToolResult, projectAssistantRuntimeWorkflowResult, error) {
		res, err := s.mcpRuntimeStatusOrPreview(ctx, id, in.Project, formatProjectAssistantRuntimeStatusResult)
		if err != nil {
			return nil, projectAssistantRuntimeWorkflowResult{}, err
		}
		return nil, *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_preview_url",
		Title:       "Get the live preview URL",
		Description: "Return the live development preview URL when the sandbox is serving traffic, or the reason it is not ready yet.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProjectInput) (*mcp.CallToolResult, projectAssistantRuntimeWorkflowResult, error) {
		res, err := s.mcpRuntimeStatusOrPreview(ctx, id, in.Project, formatProjectAssistantPreviewURLResult)
		if err != nil {
			return nil, projectAssistantRuntimeWorkflowResult{}, err
		}
		return nil, *res, nil
	})
}

// mcpRuntimeStatusOrPreview runs the shared normalize step then the given
// formatter (runtime status or preview URL), which are two-stage graph lambdas.
func (s *Server) mcpRuntimeStatusOrPreview(
	ctx context.Context,
	id identity,
	projectName string,
	format func(context.Context, projectAssistantRuntimeWorkflowInput) (*projectAssistantRuntimeWorkflowResult, error),
) (*projectAssistantRuntimeWorkflowResult, error) {
	c, p, err := s.mcpProjectContext(ctx, id, projectName)
	if err != nil {
		return nil, err
	}
	runCtx := s.mcpRunContext(id, c, p)
	normalized, err := projectAssistantRuntimeWorkflowInputFromStatusTool(runCtx)(ctx, &projectAssistantRuntimeStatusToolInput{})
	if err != nil {
		return nil, err
	}
	return format(ctx, normalized)
}

func mcpProjectSummaryOf(p *aiv1alpha1.Project) mcpProjectSummary {
	out := mcpProjectSummary{
		Name:          p.Name,
		DisplayName:   strings.TrimSpace(p.Spec.DisplayName),
		Description:   strings.TrimSpace(p.Spec.Description),
		RepositoryRef: projectLinkedRepositoryRef(p),
	}
	if p.Spec.Template != nil {
		out.Template = strings.TrimSpace(p.Spec.Template.Name)
	}
	return out
}
