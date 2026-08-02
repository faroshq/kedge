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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	asclient "github.com/faroshq/provider-app-studio/client"
	"github.com/faroshq/provider-app-studio/tenant"
	"github.com/faroshq/provider-app-studio/workspace"
)

const (
	projectDevelopmentEnvironmentName   = "development"
	projectDevelopmentBindingName       = "dev"
	projectDevelopmentProviderAppStudio = "app-studio"
	projectSandboxSyncTimeout           = 20 * time.Second
)

type projectDevelopmentSyncTargetInfo struct {
	EnvironmentName string
	BindingName     string
	Provider        string
	ResourceName    string

	// Resource / Kind / APIVersion are the instance coordinates the data
	// plane and tenant client address (the Project template's instanceCRD).
	Resource   string `json:"Resource,omitempty"`
	Kind       string `json:"Kind,omitempty"`
	APIVersion string `json:"APIVersion,omitempty"`

	// Components maps a development component name to its workspacePath, for
	// template-backed projects (docs/app-studio-template-sandboxes.md §4.2).
	// Empty means the legacy single-runner target: whole-workspace sync to
	// the instance-level verbs.
	Components map[string]projectTemplateComponent `json:"Components,omitempty"`
}

// instanceResource is the tenant.Resource descriptor for the target instance.
func (t projectDevelopmentSyncTargetInfo) instanceResource() (tenant.Resource, error) {
	gv, err := schema.ParseGroupVersion(t.APIVersion)
	if err != nil {
		return tenant.Resource{}, fmt.Errorf("target apiVersion %q: %w", t.APIVersion, err)
	}
	return providerBindingResource(gv.WithResource(t.Resource), t.Kind), nil
}

// dataPlaneRefFor addresses the target's instance, optionally scoped to a
// component.
func (t projectDevelopmentSyncTargetInfo) dataPlaneRefFor(component string) dataPlaneRef {
	return dataPlaneRef{Resource: t.Resource, Name: t.ResourceName, Component: component}
}

// sortedComponents returns the component names in deterministic order.
func (t projectDevelopmentSyncTargetInfo) sortedComponents() []string {
	names := make([]string, 0, len(t.Components))
	for name := range t.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// componentWorkspacePathSummary renders the component → workspacePath map for
// error messages, sorted by component name (e.g. "backend → api/, frontend → web/").
func (t projectDevelopmentSyncTargetInfo) componentWorkspacePathSummary() string {
	parts := make([]string, 0, len(t.Components))
	for _, name := range t.sortedComponents() {
		wp := path.Clean(strings.TrimSpace(t.Components[name].WorkspacePath))
		if wp == "." {
			parts = append(parts, name+" → the workspace root")
			continue
		}
		parts = append(parts, name+" → "+wp+"/")
	}
	return strings.Join(parts, ", ")
}

type projectSandboxSyncFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type projectSandboxSyncRequest struct {
	Files []projectSandboxSyncFile `json:"files"`
	// DeletePaths removes files the workspace no longer has. Without it a
	// renamed or deleted file keeps running in the sandbox, because the sync
	// only ever ships the files that currently exist.
	DeletePaths []string `json:"deletePaths,omitempty"`
	Restart     string   `json:"restart,omitempty"`
}

type projectDevelopmentSyncResponse struct {
	Target projectDevelopmentSyncTargetInfo `json:"target"`
	Result json.RawMessage                  `json:"result,omitempty"`
	// SkippedFiles lists workspace files the sync payload cannot carry (binary
	// or oversized). They are absent from the sandbox, so callers must be able
	// to say so rather than reporting an unqualified success.
	SkippedFiles []string `json:"skippedFiles,omitempty"`
}

type projectDevelopmentPreviewAuthorizeResponse struct {
	Target                projectDevelopmentSyncTargetInfo `json:"target"`
	Ready                 bool                             `json:"ready"`
	PreviewURL            string                           `json:"previewURL,omitempty"`
	PreviewTokenExpiresAt string                           `json:"previewTokenExpiresAt,omitempty"`
	Message               string                           `json:"message,omitempty"`
	Reason                string                           `json:"reason,omitempty"`
}

type projectSandboxPreviewURLResponse struct {
	Ready                 bool   `json:"ready"`
	PreviewURL            string `json:"previewURL,omitempty"`
	PreviewTokenExpiresAt string `json:"previewTokenExpiresAt,omitempty"`
	Message               string `json:"message,omitempty"`
	Reason                string `json:"reason,omitempty"`
}

// projectDevelopmentTarget resolves the Project's development data-plane
// target: the template instance, with the Template's component map read live
// from the tenant catalog. A project without a bound template has no
// development environment.
func (s *Server) projectDevelopmentTarget(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, _ identity) (projectDevelopmentSyncTargetInfo, error) {
	if p == nil {
		return projectDevelopmentSyncTargetInfo{}, fmt.Errorf("project is nil")
	}
	if p.Spec.Template == nil || strings.TrimSpace(p.Spec.Template.Name) == "" {
		return projectDevelopmentSyncTargetInfo{}, newValidationError("project has no development template yet — select one first")
	}
	info, err := fetchProjectTemplate(ctx, c, p.Spec.Template.Name)
	if err != nil {
		return projectDevelopmentSyncTargetInfo{}, fmt.Errorf("read project template %q: %w", p.Spec.Template.Name, err)
	}
	// A template-backed target without development components must never fall
	// through to the legacy sandbox-runner code paths — they would mis-handle
	// a non-sandbox instance. selectProjectTemplate refuses such templates;
	// this guards against the template losing its development block later.
	if len(info.Components) == 0 {
		return projectDevelopmentSyncTargetInfo{}, fmt.Errorf("project template %q no longer declares development components", info.Name)
	}
	name := projectTemplateInstanceName(p)
	if name == "" {
		return projectDevelopmentSyncTargetInfo{}, fmt.Errorf("project has no name")
	}
	return projectDevelopmentSyncTargetInfo{
		EnvironmentName: projectDevelopmentEnvironmentName,
		BindingName:     projectDevelopmentBindingName,
		Provider:        projectDevelopmentProviderAppStudio,
		Resource:        info.Resource,
		Kind:            info.Kind,
		APIVersion:      info.APIVersion,
		ResourceName:    name,
		Components:      info.Components,
	}, nil
}

func (s *Server) syncProjectDevelopment(w http.ResponseWriter, r *http.Request) {
	c, id, p, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	target, err := s.projectDevelopmentTarget(r.Context(), c, p, id)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	result, skipped, err := s.syncProjectDevelopmentTarget(r.Context(), c, id, p, target)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectDevelopmentSyncResponse{Target: target, Result: result, SkippedFiles: skipped})
}

func (s *Server) authorizeProjectDevelopmentPreview(w http.ResponseWriter, r *http.Request) {
	c, id, p, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	target, err := s.projectDevelopmentTarget(r.Context(), c, p, id)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	preview, err := s.authorizeProjectDevelopmentPreviewTarget(r.Context(), c, id, p, target)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectDevelopmentPreviewAuthorizeResponse{
		Target:                target,
		Ready:                 preview.Ready,
		PreviewURL:            preview.PreviewURL,
		PreviewTokenExpiresAt: preview.PreviewTokenExpiresAt,
		Message:               preview.Message,
		Reason:                preview.Reason,
	})
}

func (s *Server) syncProjectDevelopmentTarget(ctx context.Context, c *asclient.Client, id identity, p *aiv1alpha1.Project, target projectDevelopmentSyncTargetInfo) (json.RawMessage, []string, error) {
	if s.workspaces == nil {
		return nil, nil, fmt.Errorf("project workspace store is not configured")
	}
	files, skipped, err := s.projectWorkspaceSyncFiles(ctx, projectWorkspaceScope(id, p.Name))
	if err != nil {
		return nil, nil, err
	}
	// Validate the instance exists in the workspace first (clear 404 vs proxy err).
	if err := s.validateDevelopmentInstance(ctx, c, target); err != nil {
		return nil, nil, err
	}

	// Route files to each component's own sync verb
	// by workspacePath prefix (docs/app-studio-template-sandboxes.md §4.2).
	// Files outside every component (README, docs) sync nowhere.
	routed := routeProjectSyncFiles(files, target.Components)
	deleted, err := s.workspaces.DeletedPaths(projectWorkspaceScope(id, p.Name))
	if err != nil {
		return nil, nil, err
	}
	routedDeletes := routeProjectSyncDeletes(deleted, target.Components)
	// A populated workspace whose files all fall outside every component
	// directory would "succeed" while shipping nothing to the sandbox — the
	// app never starts and nothing explains why. Fail with the expected
	// layout instead.
	// Count only the user's own files. App Studio writes its build config and CI
	// workflow to the workspace root, so a project that has just bound a
	// template — and has nothing else yet — contains exactly those two managed
	// files and nothing routable. Counting them made the first sync of every new
	// project fail with "none of the N workspace files are under a development
	// component directory", which then surfaced as a runtime blocker and left
	// the assistant convinced it had no scaffold to work with.
	appFiles := countProjectSyncAppFiles(files)
	if appFiles > 0 && countRoutedProjectSyncFiles(routed) == 0 {
		return nil, nil, fmt.Errorf(
			"none of the %d workspace files are under a development component directory (%s); application source must live under those directories to reach the development sandbox",
			appFiles, target.componentWorkspacePathSummary())
	}
	// Files landing in the right directory but written for the wrong runtime
	// fail silently otherwise: the sandbox image has no toolchain for them, the
	// start command finds nothing to run, and the pod simply never listens.
	if err := validateProjectSyncToolchains(routed, target.Components); err != nil {
		return nil, nil, err
	}
	results := map[string]json.RawMessage{}
	for _, component := range target.sortedComponents() {
		componentFiles := routed[component]
		manifest := projectSyncManifest(componentFiles)
		// Ship only what the sandbox does not already hold. Deletions are sent
		// explicitly, so omitting unchanged files cannot remove anything.
		payloadFiles := changedProjectSyncFiles(componentFiles, s.syncedManifestFor(id, p.Name, component))
		deletes := routedDeletes[component]
		if len(payloadFiles) == 0 && len(deletes) == 0 {
			// Nothing to do for this component; record it as current and move
			// on rather than paying a round trip to say nothing changed.
			s.recordSyncedManifest(id, p.Name, component, manifest)
			continue
		}
		payload, err := json.Marshal(projectSandboxSyncRequest{
			Files:       payloadFiles,
			DeletePaths: deletes,
			Restart:     "auto",
		})
		if err != nil {
			return nil, nil, fmt.Errorf("encode %s sync payload: %w", component, err)
		}
		body, status, err := s.dataPlanePost(ctx, id, target.dataPlaneRefFor(component), dataPlaneVerbSync, payload)
		if err != nil {
			// The sandbox's contents are now unknown — it may have applied part
			// of the payload. Force the next sync to send everything.
			s.forgetSyncedManifests(id, p.Name)
			return nil, nil, fmt.Errorf("component %s: %w", component, err)
		}
		if status < 200 || status >= 300 {
			s.forgetSyncedManifests(id, p.Name)
			return nil, nil, fmt.Errorf("component %s sync returned %d: %s", component, status, strings.TrimSpace(string(body)))
		}
		s.recordSyncedManifest(id, p.Name, component, manifest)
		results[component] = json.RawMessage(body)
	}
	aggregated, err := json.Marshal(results)
	if err != nil {
		return nil, nil, err
	}
	return aggregated, skipped, nil
}

// validateDevelopmentInstance confirms the target instance exists in the
// tenant workspace so a missing instance surfaces as a Kubernetes 404 rather
// than a data-plane proxy error.
func (s *Server) validateDevelopmentInstance(ctx context.Context, c *asclient.Client, target projectDevelopmentSyncTargetInfo) error {
	res, err := target.instanceResource()
	if err != nil {
		return err
	}
	_, err = c.Resource(res, "").Get(ctx, target.ResourceName, metav1.GetOptions{})
	return err
}

// routeProjectSyncFiles groups workspace files by development component: a
// file under a component's workspacePath syncs to that component with the
// prefix stripped (the component's PVC holds only its own subtree). "." claims
// the whole workspace (single-component templates); the Template validation
// guarantees paths never nest, so a file maps to at most one component.
func routeProjectSyncFiles(files []projectSandboxSyncFile, components map[string]projectTemplateComponent) map[string][]projectSandboxSyncFile {
	out := make(map[string][]projectSandboxSyncFile, len(components))
	for component, comp := range components {
		wp := path.Clean(strings.TrimSpace(comp.WorkspacePath))
		if wp == "." {
			out[component] = files
			continue
		}
		prefix := wp + "/"
		for _, f := range files {
			if strings.HasPrefix(f.Path, prefix) {
				out[component] = append(out[component], projectSandboxSyncFile{
					Path:    strings.TrimPrefix(f.Path, prefix),
					Content: f.Content,
				})
			}
		}
	}
	return out
}

// developmentSyncManifestKey scopes a component's synced-content manifest.
func developmentSyncManifestKey(id identity, projectName, component string) string {
	return id.orgUUID + "/" + id.workspaceUUID + "/" + projectName + "/" + component
}

// projectSyncFileHash fingerprints one file's content for change detection.
func projectSyncFileHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// projectSyncManifest fingerprints a component's routed file set.
func projectSyncManifest(files []projectSandboxSyncFile) map[string]string {
	manifest := make(map[string]string, len(files))
	for _, f := range files {
		manifest[f.Path] = projectSyncFileHash(f.Content)
	}
	return manifest
}

// changedProjectSyncFiles returns the files that differ from what the sandbox
// was last confirmed to hold. A nil previous manifest means "unknown", which
// must send everything: the alternative is a sandbox silently missing files.
func changedProjectSyncFiles(files []projectSandboxSyncFile, previous map[string]string) []projectSandboxSyncFile {
	if previous == nil {
		return files
	}
	changed := make([]projectSandboxSyncFile, 0, len(files))
	for _, f := range files {
		if previous[f.Path] != projectSyncFileHash(f.Content) {
			changed = append(changed, f)
		}
	}
	return changed
}

// syncedManifestFor returns the content manifest last confirmed for one
// component, or nil when the sandbox's contents are not known.
func (s *Server) syncedManifestFor(id identity, projectName, component string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.developmentSyncManifests[developmentSyncManifestKey(id, projectName, component)]
}

// recordSyncedManifest remembers what a component now holds.
func (s *Server) recordSyncedManifest(id identity, projectName, component string, manifest map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.developmentSyncManifests == nil {
		s.developmentSyncManifests = map[string]map[string]string{}
	}
	s.developmentSyncManifests[developmentSyncManifestKey(id, projectName, component)] = manifest
}

// forgetSyncedManifests drops every component manifest for one project, so the
// next sync ships the whole workspace. Called whenever the sandbox's contents
// stop being predictable from here: a failed sync, or a template switch that
// replaces the runtime and its volume.
func (s *Server) forgetSyncedManifests(id identity, projectName string) {
	prefix := developmentSyncManifestKey(id, projectName, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.developmentSyncManifests {
		if strings.HasPrefix(key, prefix) {
			delete(s.developmentSyncManifests, key)
		}
	}
}

// routeProjectSyncDeletes maps deleted workspace paths onto the components
// that were serving them, using the same workspacePath prefix rule as
// routeProjectSyncFiles.
func routeProjectSyncDeletes(paths []string, components map[string]projectTemplateComponent) map[string][]string {
	out := make(map[string][]string, len(components))
	for component, comp := range components {
		wp := path.Clean(strings.TrimSpace(comp.WorkspacePath))
		if wp == "." {
			out[component] = append(out[component], paths...)
			continue
		}
		prefix := wp + "/"
		for _, p := range paths {
			if rest, ok := strings.CutPrefix(p, prefix); ok {
				out[component] = append(out[component], rest)
			}
		}
	}
	return out
}

// projectToolchainManifests names the file every known toolchain needs before
// its component's start command can run: without it the dev process exits
// immediately (or never starts), the port stays closed, and the only symptom is
// an app that "looks up" while every request to it fails.
//
// Keyed by the toolchain half of the template's ${kedge.devImage.<toolchain>}
// token. A toolchain absent from this map is not validated — an unknown
// toolchain must never block a sync, since the template, not App Studio, is the
// authority on what its sandbox can run.
var projectToolchainManifests = map[string]struct {
	// Files are the accepted manifest names; any one present satisfies the check.
	Files []string
	// Hint tells the caller what to write instead, in the terms the agent
	// needs to act on.
	Hint string
}{
	"node": {
		Files: []string{"package.json"},
		Hint:  "write a package.json whose \"dev\" or \"start\" script launches the server on $PORT",
	},
	"python": {
		Files: []string{"requirements.txt", "pyproject.toml", "Pipfile", "setup.py"},
		Hint:  "write a requirements.txt or pyproject.toml declaring the app's dependencies",
	},
	"go": {
		Files: []string{"go.mod"},
		Hint:  "write a go.mod at the component root",
	},
	"ruby": {
		Files: []string{"Gemfile"},
		Hint:  "write a Gemfile at the component root",
	},
}

// validateProjectSyncToolchains rejects a sync whose routed files cannot
// possibly run in the component's sandbox. It fires only when a component
// received files AND its toolchain is one we know AND that toolchain's manifest
// is absent — so an empty component (nothing written yet) and an unrecognized
// toolchain both pass untouched.
//
// This is the backstop for the contract being ignored rather than unavailable:
// the template declares the toolchain and start command, inspect_development_
// templates surfaces them, and the prompt states them — but none of that
// guarantees the generated code matches, and a mismatch is otherwise invisible
// until someone opens the app and finds nothing listening.
func validateProjectSyncToolchains(routed map[string][]projectSandboxSyncFile, components map[string]projectTemplateComponent) error {
	names := make([]string, 0, len(routed))
	for name := range routed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		files := routed[name]
		if len(files) == 0 {
			continue
		}
		comp, ok := components[name]
		if !ok {
			continue
		}
		manifest, known := projectToolchainManifests[comp.Toolchain]
		if !known {
			continue
		}
		if projectSyncFilesContainManifest(files, manifest.Files) {
			continue
		}
		where := path.Clean(strings.TrimSpace(comp.WorkspacePath))
		if where == "." {
			where = "the workspace root"
		} else {
			where += "/"
		}
		return fmt.Errorf(
			"component %q runs a %s development sandbox but %s contains no %s — %s. The sandbox has no other toolchain installed and starts this component with: %s",
			name, comp.Toolchain, where,
			humanizeProjectManifestList(manifest.Files), manifest.Hint,
			summarizeProjectStartCommand(comp.StartCommand))
	}
	return nil
}

// projectSyncFilesContainManifest reports whether any accepted manifest sits at
// the component root. Paths are already component-relative here (the router
// strips the workspacePath prefix), so a root manifest has no separator — a
// nested one (e.g. "vendor/package.json") must not satisfy the check.
func projectSyncFilesContainManifest(files []projectSandboxSyncFile, accepted []string) bool {
	for _, f := range files {
		p := path.Clean(strings.TrimSpace(f.Path))
		if strings.Contains(p, "/") {
			continue
		}
		for _, name := range accepted {
			if p == name {
				return true
			}
		}
	}
	return false
}

// humanizeProjectManifestList renders accepted manifests as "a package.json" or
// "a requirements.txt, pyproject.toml, Pipfile, or setup.py".
func humanizeProjectManifestList(files []string) string {
	switch len(files) {
	case 0:
		return "manifest"
	case 1:
		return files[0]
	case 2:
		return files[0] + " or " + files[1]
	default:
		return strings.Join(files[:len(files)-1], ", ") + ", or " + files[len(files)-1]
	}
}

// projectStartCommandSummaryMaxChars bounds a start command in an error
// message. Templates may inline a long config shim (the application template's
// frontend embeds a base64 vite config), and the useful signal is the leading
// command, not the payload.
const projectStartCommandSummaryMaxChars = 160

func summarizeProjectStartCommand(cmd string) string {
	cmd = strings.Join(strings.Fields(cmd), " ")
	if cmd == "" {
		return "(the template declares no start command)"
	}
	if len(cmd) > projectStartCommandSummaryMaxChars {
		return cmd[:projectStartCommandSummaryMaxChars] + "..."
	}
	return cmd
}

// projectSyncManagedFile reports whether a workspace path is written by App
// Studio itself rather than by the user or the assistant.
//
// These live at the workspace root by design — the build config and CI workflow
// describe the whole project, not one component — so they are legitimately
// outside every component directory and must not be mistaken for misplaced
// application source.
func projectSyncManagedFile(path string) bool {
	switch path {
	case projectBuildConfigPath, projectBuildWorkflowPath, projectScaffoldManifestPath:
		return true
	default:
		return false
	}
}

// countProjectSyncAppFiles totals the files that represent application source,
// excluding App Studio's own managed files.
func countProjectSyncAppFiles(files []projectSandboxSyncFile) int {
	total := 0
	for _, f := range files {
		if !projectSyncManagedFile(f.Path) {
			total++
		}
	}
	return total
}

// countRoutedProjectSyncFiles totals the files routed across all components.
func countRoutedProjectSyncFiles(routed map[string][]projectSandboxSyncFile) int {
	total := 0
	for _, files := range routed {
		total += len(files)
	}
	return total
}

// authorizeProjectDevelopmentPreviewTarget resolves the preview for a
// development environment: the template instance's own public URL — the dev
// overlay keeps the production route wiring, so the dev instance is served
// where a production one would be. See docs/app-studio-template-sandboxes.md §1.
func (s *Server) authorizeProjectDevelopmentPreviewTarget(ctx context.Context, c *asclient.Client, _ identity, _ *aiv1alpha1.Project, target projectDevelopmentSyncTargetInfo) (projectSandboxPreviewURLResponse, error) {
	return s.templateDevelopmentPreview(ctx, c, target)
}

// projectWorkspaceSyncFiles reads every syncable workspace file. A truncated
// listing is a hard error: shipping an alphabetical prefix of the tree leaves
// the sandbox running a partial application with nothing to explain why. Files
// the sync payload cannot carry (binary, oversized) are returned separately so
// the caller can report them rather than dropping them silently.
func (s *Server) projectWorkspaceSyncFiles(ctx context.Context, scope workspace.Scope) ([]projectSandboxSyncFile, []string, error) {
	list, err := s.workspaces.ListFiles(ctx, scope, workspace.ListOptions{Limit: workspace.MaxListLimit})
	if err != nil {
		return nil, nil, err
	}
	if list.Truncated {
		return nil, nil, fmt.Errorf(
			"workspace has more than %d files, which exceeds the development sync limit; only part of the project would reach the sandbox. Remove build output or vendored dependencies from the workspace and sync again",
			workspace.MaxListLimit)
	}
	files := make([]projectSandboxSyncFile, 0, len(list.Files))
	var skipped []string
	for _, f := range list.Files {
		read, err := s.workspaces.ReadFile(ctx, scope, workspace.ReadOptions{Path: f.Path, MaxBytes: workspace.MaxWriteBytes})
		if err != nil {
			return nil, nil, err
		}
		if read.Binary || read.Truncated {
			skipped = append(skipped, read.Path)
			continue
		}
		files = append(files, projectSandboxSyncFile{Path: read.Path, Content: read.Content})
	}
	return files, skipped, nil
}

func (s *Server) projectAssistantPreviewRefreshNeeded(_ context.Context, _ workspace.Scope, _ string, _ bool, toolCalls []projectToolCallStreamEvent) bool {
	return projectAssistantToolCallsRequireDevelopmentSync(toolCalls)
}

func shouldSyncDevelopmentAfterTool(name string) bool {
	switch projectToolBaseName(name) {
	case projectToolWriteFile, projectToolApplyPatch, projectToolDeleteFile, projectToolMkdir, projectToolSelectTemplate, projectToolHydrateWorkspace:
		return true
	default:
		return false
	}
}

func (s *Server) scheduleDevelopmentSyncAfterMutation(id identity, p *aiv1alpha1.Project, name string) {
	if s == nil || p == nil || !shouldSyncDevelopmentAfterTool(name) {
		return
	}
	// Selecting a template tears the development environment down and
	// re-provisions it, and hydrating replaces the workspace wholesale. Either
	// way what the sandbox holds is no longer derivable from the last sync, so
	// the next one must send everything.
	switch projectToolBaseName(name) {
	case projectToolSelectTemplate, projectToolHydrateWorkspace:
		s.forgetSyncedManifests(id, p.Name)
	}
	project := p.DeepCopy()
	s.mu.Lock()
	hook := s.developmentSyncAfterMutation
	s.mu.Unlock()
	if hook != nil {
		hook(id, project, name)
		return
	}
	go s.syncDevelopmentAfterMutation(id, project, name)
}

func (s *Server) syncDevelopmentAfterMutation(id identity, p *aiv1alpha1.Project, name string) {
	if s.gql == nil {
		klog.V(2).Infof("development sandbox sync after %s skipped for project %s: tenant GraphQL client is not configured", projectToolBaseName(name), p.Name)
		return
	}
	c, err := s.clientFor(id)
	if err != nil {
		klog.V(2).Infof("development sandbox sync after %s failed for project %s: %v", projectToolBaseName(name), p.Name, err)
		return
	}
	s.syncDevelopmentAfterMutationWithClient(c, id, p, name)
}

func (s *Server) syncDevelopmentAfterMutationWithClient(c *asclient.Client, id identity, p *aiv1alpha1.Project, name string) {
	lock := s.developmentSyncLock(id, p.Name)
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), projectSandboxSyncTimeout)
	defer cancel()
	target, err := s.projectDevelopmentTarget(ctx, c, p, id)
	if err != nil {
		klog.V(2).Infof("development sync after %s skipped for project %s: %v", projectToolBaseName(name), p.Name, err)
		return
	}
	_, skipped, err := s.syncProjectDevelopmentTarget(ctx, c, id, p, target)
	if err != nil {
		// A failed post-mutation sync means the user's edit never reached the
		// development sandbox — warn, don't bury it at debug verbosity, and
		// record it so the assistant's own verification reports it instead of
		// silently diagnosing a stale sandbox.
		klog.Warningf("development sync after %s failed for project %s: %v", projectToolBaseName(name), p.Name, err)
		s.recordDevelopmentSyncFailure(id, p.Name, fmt.Sprintf("the last workspace sync after %s failed, so the development sandbox is still running the previous code: %v", projectToolBaseName(name), err))
		return
	}
	if len(skipped) > 0 {
		// The sync succeeded but the sandbox does not match the workspace. Left
		// unreported this reads as a working sandbox that inexplicably 404s on
		// an asset the assistant is certain it wrote.
		s.recordDevelopmentSyncFailure(id, p.Name, fmt.Sprintf(
			"the development sandbox is missing %d workspace file(s) that the sync cannot carry (binary or larger than %d bytes): %s",
			len(skipped), workspace.MaxWriteBytes, strings.Join(skipped, ", ")))
		return
	}
	s.clearDevelopmentSyncFailure(id, p.Name)
}

// developmentSyncFailureKey scopes a recorded failure to one tenant's project,
// matching developmentSyncLock's key.
func developmentSyncFailureKey(id identity, projectName string) string {
	return id.orgUUID + "/" + id.workspaceUUID + "/" + projectName
}

// recordDevelopmentSyncFailure stores the reason the last background sync
// failed. Overwrites any previous reason: only the latest matters.
func (s *Server) recordDevelopmentSyncFailure(id identity, projectName, reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.developmentSyncFailures == nil {
		s.developmentSyncFailures = map[string]string{}
	}
	s.developmentSyncFailures[developmentSyncFailureKey(id, projectName)] = reason
}

// clearDevelopmentSyncFailure drops a recorded failure once a sync succeeds,
// so a stale blocker never outlives the problem it described.
func (s *Server) clearDevelopmentSyncFailure(id identity, projectName string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.developmentSyncFailures, developmentSyncFailureKey(id, projectName))
}

// lastDevelopmentSyncFailure returns the recorded failure for a project, if any.
func (s *Server) lastDevelopmentSyncFailure(id identity, projectName string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.developmentSyncFailures[developmentSyncFailureKey(id, projectName)]
}
