/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	asclient "github.com/faroshq/provider-app-studio/client"
	"github.com/faroshq/provider-app-studio/workspace"
)

func TestDefaultProjectDevelopmentEnvironmentUsesSandboxLiveBinding(t *testing.T) {
	env := defaultProjectDevelopmentEnvironment("todo")
	if got, want := env.Name, "development"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := env.Mode, aiv1alpha1.ProjectEnvironmentModeLive; got != want {
		t.Fatalf("Mode = %q, want %q", got, want)
	}
	if got := len(env.Bindings); got != 1 {
		t.Fatalf("bindings = %d, want 1", got)
	}
	binding := env.Bindings[0]
	if got, want := binding.Provider, "sandbox"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := binding.ResourceRef.APIVersion, "sandbox.kedge.faros.sh/v1alpha1"; got != want {
		t.Fatalf("APIVersion = %q, want %q", got, want)
	}
	if got, want := binding.ResourceRef.Name, "todo-dev"; got != want {
		t.Fatalf("ResourceRef.Name = %q, want %q", got, want)
	}
	var values map[string]any
	if err := json.Unmarshal(binding.Values.Raw, &values); err != nil {
		t.Fatalf("unmarshal binding values: %v", err)
	}
	if _, ok := values["runtime"]; ok {
		t.Fatalf("binding values should not expose sandbox runtime defaults: %#v", values)
	}
	if got, want := values["projectRef"], "todo"; got != want {
		t.Fatalf("binding values projectRef = %q, want %q", got, want)
	}
}

func TestProjectAssistantRuntimePreviewURLPrefersDevelopment(t *testing.T) {
	p := &aiv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "todo"},
		Status: aiv1alpha1.ProjectStatus{
			Environments: []aiv1alpha1.ProjectEnvironmentStatus{
				{
					Name: "test",
					Bindings: []aiv1alpha1.ProjectProviderBindingStatus{{
						Name:       "web",
						PreviewURL: "/test",
					}},
				},
				{
					Name: "development",
					Mode: aiv1alpha1.ProjectEnvironmentModeLive,
					Bindings: []aiv1alpha1.ProjectProviderBindingStatus{{
						Name:       "dev",
						Provider:   "sandbox",
						PreviewURL: "/dev",
					}},
				},
			},
		},
	}
	if got, want := projectAssistantRuntimePreviewURL(p), "/dev"; got != want {
		t.Fatalf("preview URL = %q, want %q", got, want)
	}
}

func TestCreateProjectSpecIncludesDevelopmentEnvironment(t *testing.T) {
	spec := defaultProjectSpec("todo", "Todo", "Tasks", &aiv1alpha1.ProjectRepositoryBinding{RepositoryRef: "todo"})
	if got := len(spec.Environments); got != 1 {
		t.Fatalf("environments = %d, want 1", got)
	}
	if got, want := spec.Environments[0].Name, "development"; got != want {
		t.Fatalf("environment name = %q, want %q", got, want)
	}
}

func TestProjectDevelopmentSyncTargetReadsSandboxBindingName(t *testing.T) {
	p := &aiv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "todo"},
		Spec: aiv1alpha1.ProjectSpec{
			Environments: []aiv1alpha1.ProjectEnvironmentSpec{defaultProjectDevelopmentEnvironment("todo")},
		},
	}
	target, ok := projectDevelopmentSyncTarget(p)
	if !ok {
		t.Fatal("projectDevelopmentSyncTarget returned !ok")
	}
	if got, want := target.Provider, "sandbox"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := target.EnvironmentName, "development"; got != want {
		t.Fatalf("EnvironmentName = %q, want %q", got, want)
	}
	if got, want := target.BindingName, "dev"; got != want {
		t.Fatalf("BindingName = %q, want %q", got, want)
	}
	if got, want := target.ResourceName, "todo-dev"; got != want {
		t.Fatalf("ResourceName = %q, want %q", got, want)
	}
}

func TestSyncProjectDevelopmentTargetPostsWorkspaceFilesToSandbox(t *testing.T) {
	var gotAuth string
	var gotTenant string
	var gotFiles []map[string]string
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/services/providers/sandbox/api/dev-environments/todo-dev/sync"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		gotAuth = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Kedge-Tenant")
		var body struct {
			Files   []map[string]string `json:"files"`
			Restart string              `json:"restart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode sync request: %v", err)
		}
		if got, want := body.Restart, "auto"; got != want {
			t.Fatalf("restart = %q, want %q", got, want)
		}
		gotFiles = body.Files
		fmt.Fprint(w, `{"phase":"Synced","changed":["src/App.tsx"]}`)
	}))
	defer sandbox.Close()

	workspaces := workspace.NewFileStore(t.TempDir())
	id := identity{tenantPath: "root:kedge:tenants:org-a:ws-1", orgUUID: "org-a", workspaceUUID: "ws-1", token: "caller-token"}
	project := &aiv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "todo"},
		Spec: aiv1alpha1.ProjectSpec{
			Environments: []aiv1alpha1.ProjectEnvironmentSpec{defaultProjectDevelopmentEnvironment("todo")},
		},
	}
	scope := projectWorkspaceScope(id, project.Name)
	if err := workspaces.ApplyFiles(context.Background(), scope, []workspace.File{{Path: "src/App.tsx", Content: "hello\n"}}); err != nil {
		t.Fatalf("ApplyFiles returned error: %v", err)
	}

	server := NewWithWorkspace(nil, nil, workspaces, sandbox.URL, false)
	target, ok := projectDevelopmentSyncTarget(project)
	if !ok {
		t.Fatal("projectDevelopmentSyncTarget returned !ok")
	}
	if _, err := server.syncProjectDevelopmentTarget(context.Background(), id, project, target); err != nil {
		t.Fatalf("syncProjectDevelopmentTarget returned error: %v", err)
	}
	if got, want := gotAuth, "Bearer caller-token"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := gotTenant, id.tenantPath; got != want {
		t.Fatalf("X-Kedge-Tenant = %q, want %q", got, want)
	}
	if len(gotFiles) != 1 || gotFiles[0]["path"] != "src/App.tsx" || gotFiles[0]["content"] != "hello\n" {
		t.Fatalf("files = %#v, want src/App.tsx content", gotFiles)
	}
}

func TestReconcileProjectLiveBindingsCreatesSandboxDevEnvironment(t *testing.T) {
	client := asclient.NewFromDynamic(fake.NewSimpleDynamicClient(runtime.NewScheme(), &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "ai.kedge.faros.sh/v1alpha1",
			"kind":       "Project",
			"metadata": map[string]any{
				"name": "todo",
			},
			"spec": map[string]any{
				"displayName": "Todo",
				"environments": []any{map[string]any{
					"name": "development",
					"mode": "live",
					"bindings": []any{map[string]any{
						"name":     "dev",
						"provider": "sandbox",
						"kind":     "providerResource",
						"resourceRef": map[string]any{
							"name":       "todo-dev",
							"apiVersion": "sandbox.kedge.faros.sh/v1alpha1",
							"kind":       "DevEnvironment",
							"resource":   "devenvironments",
						},
						"values": map[string]any{
							"projectRef": "todo",
						},
					}},
				}},
			},
		},
	}))
	project := &aiv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "todo"},
		Spec:       defaultProjectSpec("todo", "Todo", "", nil),
	}
	server := NewWithWorkspace(nil, nil, nil, "http://hub.example", false)
	if _, err := server.reconcileProjectLiveBindings(context.Background(), client, project); err != nil {
		t.Fatalf("reconcileProjectLiveBindings returned error: %v", err)
	}
	obj, err := client.Dynamic().Resource(sandboxDevEnvironmentGVR()).Get(context.Background(), "todo-dev", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get DevEnvironment returned error: %v", err)
	}
	if got, _, _ := unstructured.NestedString(obj.Object, "spec", "projectRef"); got != "todo" {
		t.Fatalf("spec.projectRef = %q, want todo", got)
	}
	if _, ok, _ := unstructured.NestedString(obj.Object, "spec", "runtime", "image"); ok {
		t.Fatalf("runtime.image should be defaulted by provider-sandbox, not App Studio")
	}
}

func TestProjectViewOverlaysSandboxPreviewStatus(t *testing.T) {
	project := &aiv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "todo"},
		Spec:       defaultProjectSpec("todo", "Todo", "", nil),
	}
	devEnv := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "sandbox.kedge.faros.sh/v1alpha1",
		"kind":       "DevEnvironment",
		"metadata": map[string]any{
			"name": "todo-dev",
		},
		"status": map[string]any{
			"phase":      "Running",
			"previewURL": "/services/providers/sandbox/api/dev-environments/todo-dev/preview/",
		},
	}}
	client := asclient.NewFromDynamic(fake.NewSimpleDynamicClient(runtime.NewScheme(), devEnv))
	view := projectView(context.Background(), client, project)
	if len(view.Environments) != 1 || len(view.Environments[0].Bindings) != 1 {
		t.Fatalf("view environments = %#v, want one development binding", view.Environments)
	}
	binding := view.Environments[0].Bindings[0]
	if got, want := binding.PreviewURL, "/services/providers/sandbox/api/dev-environments/todo-dev/preview/"; got != want {
		t.Fatalf("PreviewURL = %q, want %q", got, want)
	}
	if got, want := view.Environments[0].Phase, "Running"; got != want {
		t.Fatalf("environment phase = %q, want %q", got, want)
	}
}

func sandboxDevEnvironmentGVR() k8sschema.GroupVersionResource {
	return k8sschema.GroupVersionResource{
		Group:    "sandbox.kedge.faros.sh",
		Version:  "v1alpha1",
		Resource: "devenvironments",
	}
}
