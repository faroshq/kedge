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
	"strings"
	"testing"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/store"
	"github.com/faroshq/provider-app-studio/workspace"
)

// devComponentPaths builds a component map from name → workspacePath for tests
// that only exercise path routing. Tests covering the toolchain contract build
// projectTemplateComponent values directly.
func devComponentPaths(paths map[string]string) map[string]projectTemplateComponent {
	out := make(map[string]projectTemplateComponent, len(paths))
	for name, wp := range paths {
		out[name] = projectTemplateComponent{WorkspacePath: wp}
	}
	return out
}

func TestRouteProjectSyncFilesDropsFilesOutsideEveryComponent(t *testing.T) {
	components := devComponentPaths(map[string]string{"backend": "api", "frontend": "web"})
	files := []projectSandboxSyncFile{
		{Path: "backend/index.js", Content: "x"},
		{Path: "frontend/src/App.jsx", Content: "y"},
		{Path: "README.md", Content: "z"},
	}

	routed := routeProjectSyncFiles(files, components)

	if got := countRoutedProjectSyncFiles(routed); got != 0 {
		t.Fatalf("countRoutedProjectSyncFiles = %d, want 0 — files under directories matching no component workspacePath must route nowhere", got)
	}
}

func TestRouteProjectSyncFilesCountsRoutedFiles(t *testing.T) {
	components := devComponentPaths(map[string]string{"backend": "api", "frontend": "web"})
	files := []projectSandboxSyncFile{
		{Path: "api/index.js", Content: "x"},
		{Path: "web/src/App.jsx", Content: "y"},
		{Path: "README.md", Content: "z"},
	}

	routed := routeProjectSyncFiles(files, components)

	if got := countRoutedProjectSyncFiles(routed); got != 2 {
		t.Fatalf("countRoutedProjectSyncFiles = %d, want 2", got)
	}
	if len(routed["backend"]) != 1 || routed["backend"][0].Path != "index.js" {
		t.Errorf("backend routed = %+v, want [index.js]", routed["backend"])
	}
	if len(routed["frontend"]) != 1 || routed["frontend"][0].Path != "src/App.jsx" {
		t.Errorf("frontend routed = %+v, want [src/App.jsx]", routed["frontend"])
	}
}

func TestComponentWorkspacePathSummary(t *testing.T) {
	target := projectDevelopmentSyncTargetInfo{
		Components: devComponentPaths(map[string]string{"frontend": "web", "backend": "api"}),
	}
	if got, want := target.componentWorkspacePathSummary(), "backend → api/, frontend → web/"; got != want {
		t.Errorf("componentWorkspacePathSummary = %q, want %q", got, want)
	}

	root := projectDevelopmentSyncTargetInfo{Components: devComponentPaths(map[string]string{"app": "."})}
	if got, want := root.componentWorkspacePathSummary(), "app → the workspace root"; got != want {
		t.Errorf("componentWorkspacePathSummary = %q, want %q", got, want)
	}
}

func TestProjectAssistantTemplateComponentsIsNilSafe(t *testing.T) {
	ctx := context.Background()

	if got := projectAssistantTemplateComponents(ctx, projectAssistantRunRequest{}); got != nil {
		t.Errorf("nil project/client: got %v, want nil", got)
	}
	if got := projectAssistantTemplateComponents(ctx, projectAssistantRunRequest{
		Project: &aiv1alpha1.Project{},
	}); got != nil {
		t.Errorf("project without template: got %v, want nil", got)
	}
	if got := projectAssistantTemplateComponents(ctx, projectAssistantRunRequest{
		Project: &aiv1alpha1.Project{
			Spec: aiv1alpha1.ProjectSpec{Template: &aiv1alpha1.ProjectTemplateSpec{Name: "  "}},
		},
	}); got != nil {
		t.Errorf("blank template name: got %v, want nil", got)
	}
}

func TestSystemPromptCarriesComponentDirectoryContract(t *testing.T) {
	p := &aiv1alpha1.Project{}
	p.Name = "demo"
	p.Spec.Template = &aiv1alpha1.ProjectTemplateSpec{Name: "application"}

	prompt := projectSystemPrompt(p, nil)

	for _, required := range []string{
		"developmentComponents",
		"NEVER synced to the development sandbox",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("system prompt for a template-backed project missing %q", required)
		}
	}
}

// The Go-in-a-Node-sandbox failure: source lands in the right directory, so the
// existing "nothing routed anywhere" check passes, but the sandbox image has no
// toolchain for it. Without this guard the sync succeeds, the dev process never
// listens, and the only symptom is an app whose API silently returns nothing.
func TestValidateProjectSyncToolchainsRejectsWrongLanguageSource(t *testing.T) {
	components := map[string]projectTemplateComponent{
		"backend": {WorkspacePath: "api", Toolchain: "node", StartCommand: "npm run dev || npm start"},
	}
	routed := map[string][]projectSandboxSyncFile{
		"backend": {
			{Path: "main.go", Content: "package main"},
			{Path: "go.mod", Content: "module app"},
			{Path: "Dockerfile", Content: "FROM golang"},
		},
	}

	err := validateProjectSyncToolchains(routed, components)
	if err == nil {
		t.Fatal("validateProjectSyncToolchains = nil, want an error for Go source in a node component")
	}
	for _, want := range []string{"backend", "node", "api/", "package.json", "npm run dev || npm start"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q — the message must name the component, its toolchain, and what to write", err, want)
		}
	}
}

func TestValidateProjectSyncToolchainsAcceptsMatchingSource(t *testing.T) {
	components := map[string]projectTemplateComponent{
		"backend":  {WorkspacePath: "api", Toolchain: "node", StartCommand: "npm run dev"},
		"frontend": {WorkspacePath: "web", Toolchain: "node", StartCommand: "npm run dev"},
	}
	routed := map[string][]projectSandboxSyncFile{
		"backend":  {{Path: "package.json", Content: "{}"}, {Path: "server.js", Content: "x"}},
		"frontend": {{Path: "package.json", Content: "{}"}},
	}
	if err := validateProjectSyncToolchains(routed, components); err != nil {
		t.Fatalf("validateProjectSyncToolchains = %v, want nil", err)
	}
}

// A component nobody has written to yet must not block the sync — that is the
// normal state while an assistant builds one component at a time.
func TestValidateProjectSyncToolchainsIgnoresEmptyComponents(t *testing.T) {
	components := map[string]projectTemplateComponent{
		"backend":  {WorkspacePath: "api", Toolchain: "node"},
		"frontend": {WorkspacePath: "web", Toolchain: "node"},
	}
	routed := map[string][]projectSandboxSyncFile{
		"frontend": {{Path: "package.json", Content: "{}"}},
	}
	if err := validateProjectSyncToolchains(routed, components); err != nil {
		t.Fatalf("validateProjectSyncToolchains = %v, want nil for an untouched component", err)
	}
}

// The template, not App Studio, is the authority on what its sandbox can run:
// an unrecognized toolchain must never block a sync.
func TestValidateProjectSyncToolchainsSkipsUnknownToolchain(t *testing.T) {
	components := map[string]projectTemplateComponent{
		"backend": {WorkspacePath: "api", Toolchain: "elixir"},
		"other":   {WorkspacePath: "svc"}, // template declared no parseable devImage
	}
	routed := map[string][]projectSandboxSyncFile{
		"backend": {{Path: "main.ex", Content: "x"}},
		"other":   {{Path: "main.bin", Content: "x"}},
	}
	if err := validateProjectSyncToolchains(routed, components); err != nil {
		t.Fatalf("validateProjectSyncToolchains = %v, want nil for an unknown toolchain", err)
	}
}

// A manifest must sit at the component root: the dev process runs there, so a
// nested one (a vendored dependency, a subpackage) does not make it runnable.
func TestValidateProjectSyncToolchainsRequiresRootManifest(t *testing.T) {
	components := map[string]projectTemplateComponent{
		"backend": {WorkspacePath: "api", Toolchain: "node", StartCommand: "npm start"},
	}
	routed := map[string][]projectSandboxSyncFile{
		"backend": {{Path: "vendor/dep/package.json", Content: "{}"}},
	}
	if err := validateProjectSyncToolchains(routed, components); err == nil {
		t.Fatal("validateProjectSyncToolchains = nil, want an error when package.json is only nested")
	}
}

func TestProjectTemplateToolchain(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"${kedge.devImage.node}", "node"},
		{"  ${kedge.devImage.python}  ", "python"},
		{"${kedge.devImage.dotnet-8}", "dotnet-8"},
		{"docker.io/library/node:22-bookworm", ""}, // a literal image is not a token
		{"${kedge.devAgentImage}", ""},
		{"", ""},
	} {
		if got := projectTemplateToolchain(tc.in); got != tc.want {
			t.Errorf("projectTemplateToolchain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A background sync failure used to reach klog and nothing else, so the sandbox
// kept serving stale code while the assistant verified it as healthy and
// debugged source that was never deployed. Verification must lead with it.
func TestDevelopmentSyncFailureSurfacesAsVerificationBlocker(t *testing.T) {
	server := NewWithWorkspace(nil, store.NewMemoryStore(), workspace.NewFileStore(t.TempDir()), "", false)
	id := identity{orgUUID: "org-a", workspaceUUID: "ws-1"}
	project := &aiv1alpha1.Project{}
	project.Name = "demo"

	if got := server.lastDevelopmentSyncFailure(id, project.Name); got != "" {
		t.Fatalf("lastDevelopmentSyncFailure = %q, want empty before any failure", got)
	}

	server.recordDevelopmentSyncFailure(id, project.Name, "the last workspace sync after write_file failed: boom")
	runCtx := projectAssistantWorkflowRunContext{Server: server, Project: project, Identity: id}
	if got := projectAssistantLastSyncFailure(runCtx); !strings.Contains(got, "boom") {
		t.Fatalf("projectAssistantLastSyncFailure = %q, want the recorded reason", got)
	}

	result, err := formatProjectAssistantRuntimeVerification(context.Background(), &projectAssistantRuntimeVerificationContext{
		RunContext: runCtx,
		Runtime: &projectAssistantRuntimeWorkflowResult{
			Status:     "ready",
			Summary:    "runtime is ready",
			PreviewURL: "https://demo.example",
		},
	})
	if err != nil {
		t.Fatalf("formatProjectAssistantRuntimeVerification: %v", err)
	}
	if result.Status != "not_ready" {
		t.Errorf("status = %q, want not_ready — a ready sandbox running stale code is not verified", result.Status)
	}
	if len(result.Blockers) == 0 || !strings.Contains(result.Blockers[0], "boom") {
		t.Errorf("blockers = %v, want the sync failure reported first", result.Blockers)
	}

	// A later successful sync must clear it, so the blocker never outlives the
	// problem it described.
	server.clearDevelopmentSyncFailure(id, project.Name)
	result, err = formatProjectAssistantRuntimeVerification(context.Background(), &projectAssistantRuntimeVerificationContext{
		RunContext: runCtx,
		Runtime: &projectAssistantRuntimeWorkflowResult{
			Status:     "ready",
			Summary:    "runtime is ready",
			PreviewURL: "https://demo.example",
		},
	})
	if err != nil {
		t.Fatalf("formatProjectAssistantRuntimeVerification after clear: %v", err)
	}
	for _, b := range result.Blockers {
		if strings.Contains(b, "boom") {
			t.Errorf("blockers = %v, want the cleared sync failure gone", result.Blockers)
		}
	}
}

func TestChangedProjectSyncFilesSendsOnlyModifiedContent(t *testing.T) {
	files := []projectSandboxSyncFile{
		{Path: "src/App.tsx", Content: "one\n"},
		{Path: "src/util.ts", Content: "two\n"},
	}
	manifest := projectSyncManifest(files)

	if changed := changedProjectSyncFiles(files, manifest); len(changed) != 0 {
		t.Fatalf("changed = %v, want none when nothing was edited", changed)
	}

	edited := []projectSandboxSyncFile{
		{Path: "src/App.tsx", Content: "one changed\n"},
		{Path: "src/util.ts", Content: "two\n"},
		{Path: "src/new.ts", Content: "three\n"},
	}
	changed := changedProjectSyncFiles(edited, manifest)
	if len(changed) != 2 {
		t.Fatalf("changed = %v, want the edited and the new file only", changed)
	}
	got := map[string]bool{}
	for _, f := range changed {
		got[f.Path] = true
	}
	if !got["src/App.tsx"] || !got["src/new.ts"] {
		t.Fatalf("changed paths = %v, want src/App.tsx and src/new.ts", got)
	}
}

// TestChangedProjectSyncFilesSendsEverythingWhenSandboxStateIsUnknown is the
// safety property: with no manifest the sandbox's contents are unknown, and
// sending a delta would leave it silently missing files.
func TestChangedProjectSyncFilesSendsEverythingWhenSandboxStateIsUnknown(t *testing.T) {
	files := []projectSandboxSyncFile{
		{Path: "a.txt", Content: "one\n"},
		{Path: "b.txt", Content: "two\n"},
	}
	if changed := changedProjectSyncFiles(files, nil); len(changed) != len(files) {
		t.Fatalf("changed = %v, want a full sync when no manifest is known", changed)
	}
}

func TestForgetSyncedManifestsIsScopedToOneProject(t *testing.T) {
	s := &Server{}
	id := identity{orgUUID: "org-a", workspaceUUID: "ws-1"}
	s.recordSyncedManifest(id, "shop", "web", map[string]string{"a.txt": "hash"})
	s.recordSyncedManifest(id, "shop", "api", map[string]string{"b.txt": "hash"})
	s.recordSyncedManifest(id, "blog", "web", map[string]string{"c.txt": "hash"})

	s.forgetSyncedManifests(id, "shop")

	if s.syncedManifestFor(id, "shop", "web") != nil || s.syncedManifestFor(id, "shop", "api") != nil {
		t.Fatal("shop manifests survived invalidation")
	}
	if s.syncedManifestFor(id, "blog", "web") == nil {
		t.Fatal("an unrelated project's manifest was invalidated")
	}
}

// TestTemplateSwitchInvalidatesSyncManifest pins the case that would silently
// break a sandbox: selecting a template re-provisions the runtime and its
// volume, so the previous manifest no longer describes anything.
func TestTemplateSwitchInvalidatesSyncManifest(t *testing.T) {
	s := &Server{developmentSyncAfterMutation: func(identity, *aiv1alpha1.Project, string) {}}
	id := identity{orgUUID: "org-a", workspaceUUID: "ws-1"}
	project := &aiv1alpha1.Project{}
	project.Name = "shop"
	s.recordSyncedManifest(id, "shop", "web", map[string]string{"a.txt": "hash"})

	s.scheduleDevelopmentSyncAfterMutation(id, project, projectToolSelectTemplate)
	if s.syncedManifestFor(id, "shop", "web") != nil {
		t.Fatal("manifest survived a template switch; the next sync would ship a delta to a fresh volume")
	}

	// An ordinary edit keeps the manifest, which is what makes delta sync work.
	s.recordSyncedManifest(id, "shop", "web", map[string]string{"a.txt": "hash"})
	s.scheduleDevelopmentSyncAfterMutation(id, project, projectToolWriteFile)
	if s.syncedManifestFor(id, "shop", "web") == nil {
		t.Fatal("an ordinary write invalidated the manifest, forcing needless full syncs")
	}
}

// TestSyncGuardIgnoresAppStudioManagedFiles pins the first-sync path of every
// new project. Binding a template writes the build config and CI workflow to
// the workspace root; with no app source yet, those were the only files present
// and the "nothing routed" guard failed the sync, which surfaced as a runtime
// blocker and convinced the assistant it had no scaffold to work with.
func TestSyncGuardIgnoresAppStudioManagedFiles(t *testing.T) {
	managedOnly := []projectSandboxSyncFile{
		{Path: projectBuildConfigPath, Content: "{}"},
		{Path: projectBuildWorkflowPath, Content: "name: build"},
	}
	if got := countProjectSyncAppFiles(managedOnly); got != 0 {
		t.Fatalf("app file count = %d for a freshly templated project, want 0", got)
	}

	// Real source outside every component directory must still be caught: that
	// is the misplaced-source case the guard exists for.
	withStraySource := append(append([]projectSandboxSyncFile(nil), managedOnly...),
		projectSandboxSyncFile{Path: "index.js", Content: "console.log(1)"})
	if got := countProjectSyncAppFiles(withStraySource); got != 1 {
		t.Fatalf("app file count = %d with one stray source file, want 1", got)
	}

	// And correctly placed source counts too.
	placed := []projectSandboxSyncFile{{Path: "api/src/index.js", Content: "x"}}
	if got := countProjectSyncAppFiles(placed); got != 1 {
		t.Fatalf("app file count = %d for placed source, want 1", got)
	}
}
