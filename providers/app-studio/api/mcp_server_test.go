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

import (
	"net/http"
	"testing"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
)

func TestMCPIdentityFromRequest(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	r.Header.Set("X-Kedge-Tenant", "root:kedge:tenants:org-123:ws-456")
	r.Header.Set("X-Kedge-Cluster", "cluster-abc")
	r.Header.Set("X-Kedge-User", "alice")
	r.Header.Set("Authorization", "Bearer tok-xyz")

	id := mcpIdentityFromRequest(r)
	if id.tenantPath != "root:kedge:tenants:org-123:ws-456" {
		t.Fatalf("tenantPath = %q", id.tenantPath)
	}
	if id.clusterID != "cluster-abc" {
		t.Fatalf("clusterID = %q", id.clusterID)
	}
	if id.user != "alice" {
		t.Fatalf("user = %q", id.user)
	}
	if id.token != "tok-xyz" {
		t.Fatalf("token = %q", id.token)
	}
	// org/workspace are parsed from the tenant path.
	if id.orgUUID != "org-123" || id.workspaceUUID != "ws-456" {
		t.Fatalf("org/ws = %q/%q", id.orgUUID, id.workspaceUUID)
	}
}

func TestMCPIdentityFromRequestEmptyTenant(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	id := mcpIdentityFromRequest(r)
	if id.tenantPath != "" {
		t.Fatalf("expected empty tenant, got %q", id.tenantPath)
	}
}

func TestMCPProjectSummaryOf(t *testing.T) {
	p := &aiv1alpha1.Project{}
	p.Name = "todo-app"
	p.Spec.DisplayName = "Todo App"
	p.Spec.Description = "a todo list"
	p.Spec.Template = &aiv1alpha1.ProjectTemplateSpec{Name: "application"}
	p.Spec.Repository = &aiv1alpha1.ProjectRepositoryBinding{RepositoryRef: "repo-1"}

	got := mcpProjectSummaryOf(p)
	if got.Name != "todo-app" || got.DisplayName != "Todo App" {
		t.Fatalf("unexpected summary: %+v", got)
	}
	if got.Template != "application" {
		t.Fatalf("template = %q", got.Template)
	}
	if got.RepositoryRef != "repo-1" {
		t.Fatalf("repositoryRef = %q", got.RepositoryRef)
	}
	// list_projects leaves URLs empty (get_project resolves them).
	if got.CloneURL != "" || got.HTMLURL != "" {
		t.Fatalf("summary should not carry URLs: %+v", got)
	}
}

func TestMCPHandlerBuildsServerWithoutPanic(t *testing.T) {
	s := &Server{}
	handler := s.MCPHandler()
	if handler == nil {
		t.Fatal("MCPHandler returned nil")
	}
	// Building the per-request server registers all tools; it must not touch
	// clients/workspaces (those are only used inside tool handlers), so a
	// zero-value Server is enough to exercise registration.
	r, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	if srv := s.newMCPServer(r); srv == nil {
		t.Fatal("newMCPServer returned nil")
	}
}
