/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Template scaffold materialization: when a template declaring
// spec.development.scaffold is bound to a project whose workspace is empty,
// the scaffold repository's tree (at the pinned tag) is written into the
// workspace as the initial app skeleton. The scaffold is a WORKING app
// honoring the template's platform contract, so the assistant starts every
// project by editing running code instead of bootstrapping from prose —
// launch/serve wiring mistakes (ports, same-origin /api, DATABASE_URL) are
// the dominant failure class for generated apps, and a green starting point
// removes most of them (docs/app-studio-wiring-reliability.md, Phase 0).

package api

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"k8s.io/klog/v2"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/workspace"
)

// projectTemplateScaffold mirrors Template.spec.development.scaffold: the git
// repository holding starter code for projects built on the template, pinned
// to a tag so scaffold-repo changes are inert until the template ref is
// bumped.
type projectTemplateScaffold struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref,omitempty"`
}

// projectScaffoldMaterialization reports what scaffold materialization did,
// for the template-selection responses (portal + assistant tool).
type projectScaffoldMaterialization struct {
	Repository string   `json:"repository"`
	Ref        string   `json:"ref,omitempty"`
	Written    []string `json:"written,omitempty"`
	Skipped    []string `json:"skipped,omitempty"`
	// Committed reports whether the scaffold tree also landed in the
	// project's git repository. The repo is the durable source of truth AND
	// what CI builds production images from — a scaffold that only exists in
	// the workspace would produce broken builds (the assistant's commit tool
	// sends an explicit path list and cannot be trusted to include files it
	// did not write).
	Committed   bool   `json:"committed,omitempty"`
	CommitError string `json:"commitError,omitempty"`
	// Error carries a best-effort failure (network, bad archive). Template
	// selection succeeds regardless; the workspace just starts empty.
	Error string `json:"error,omitempty"`
}

// projectScaffoldManifestPath records, per materialized path, the sha256 of
// the content the scaffold shipped. write_file consults it to allow replacing
// a still-pristine scaffold file even under create-only mutation safety —
// overwriting a file nobody has edited loses nothing, and forcing apply_patch
// for a wholesale rewrite of a starter file just produces failed-action noise.
const projectScaffoldManifestPath = ".kedge/scaffold.json"

type projectScaffoldManifest struct {
	Repository string            `json:"repository"`
	Ref        string            `json:"ref,omitempty"`
	Files      map[string]string `json:"files"`
}

const (
	// projectScaffoldMaxArchiveBytes bounds the compressed archive download.
	projectScaffoldMaxArchiveBytes = 16 << 20
	// projectScaffoldMaxFileBytes bounds one extracted file; larger files are
	// skipped (the workspace store enforces its own bounds on top).
	projectScaffoldMaxFileBytes = 1 << 20
	// projectScaffoldMaxTotalBytes bounds the extracted tree.
	projectScaffoldMaxTotalBytes = 8 << 20
	// projectScaffoldMaxFiles bounds the extracted file count.
	projectScaffoldMaxFiles = 400
)

var projectScaffoldHTTPClient = &http.Client{Timeout: 60 * time.Second}

// projectScaffoldFile is one text file extracted from the scaffold archive,
// path relative to the repository root.
type projectScaffoldFile struct {
	Path    string
	Content string
}

// projectScaffoldArchiveURLs returns the candidate tarball URLs for a
// scaffold repository + ref, most specific first. github.com gets the
// codeload endpoints (tags, then branches, then bare ref); other hosts get
// the gitea/forgejo-style /archive/<ref>.tar.gz. No git binary exists in the
// runtime image, so tarball-over-HTTPS is the only fetch path.
func projectScaffoldArchiveURLs(scaffold projectTemplateScaffold) ([]string, error) {
	repo := strings.TrimSuffix(strings.TrimSpace(scaffold.Repository), "/")
	repo = strings.TrimSuffix(repo, ".git")
	u, err := url.Parse(repo)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("scaffold repository %q is not an https git URL", scaffold.Repository)
	}
	ref := strings.TrimSpace(scaffold.Ref)
	if strings.EqualFold(u.Host, "github.com") {
		trimmed := strings.Trim(u.Path, "/")
		if strings.Count(trimmed, "/") != 1 {
			return nil, fmt.Errorf("scaffold repository %q is not a github.com owner/repo URL", scaffold.Repository)
		}
		base := "https://codeload.github.com/" + trimmed + "/tar.gz/"
		if ref == "" {
			return []string{base + "refs/heads/main"}, nil
		}
		return []string{
			base + "refs/tags/" + url.PathEscape(ref),
			base + "refs/heads/" + url.PathEscape(ref),
			base + url.PathEscape(ref),
		}, nil
	}
	if ref == "" {
		ref = "main"
	}
	return []string{repo + "/archive/" + url.PathEscape(ref) + ".tar.gz"}, nil
}

// projectScaffoldSkippedPath reports whether a repository-relative path is
// excluded from materialization. The scaffold repo's own CI, license, and
// README describe the scaffold, not the user's project — carrying its
// workflows over would run the scaffold's image-publishing pipeline inside
// the user's repository, next to the build workflow App Studio manages.
// AGENTS.md is deliberately kept: it states the platform contract the
// assistant must honor.
func projectScaffoldSkippedPath(p string) bool {
	if p == "LICENSE" || p == "README.md" {
		return true
	}
	for _, prefix := range []string{".git/", ".github/"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// fetchProjectScaffoldFiles downloads the scaffold archive and extracts its
// text tree, first path component (the archive's root directory) stripped.
func fetchProjectScaffoldFiles(ctx context.Context, scaffold projectTemplateScaffold) ([]projectScaffoldFile, []string, error) {
	urls, err := projectScaffoldArchiveURLs(scaffold)
	if err != nil {
		return nil, nil, err
	}
	var lastErr error
	for _, u := range urls {
		files, skipped, err := fetchProjectScaffoldArchive(ctx, u)
		if err == nil {
			return files, skipped, nil
		}
		lastErr = err
	}
	return nil, nil, lastErr
}

func fetchProjectScaffoldArchive(ctx context.Context, archiveURL string) ([]projectScaffoldFile, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := projectScaffoldHTTPClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("GET %s: %s", archiveURL, resp.Status)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, projectScaffoldMaxArchiveBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", archiveURL, err)
	}
	defer gz.Close()

	var files []projectScaffoldFile
	var skipped []string
	total := 0
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", archiveURL, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Strip the archive root dir ("<repo>-<ref>/") and normalize.
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		} else {
			continue
		}
		if name == "" || strings.HasPrefix(name, "..") {
			continue
		}
		if projectScaffoldSkippedPath(name) {
			continue
		}
		if len(files) >= projectScaffoldMaxFiles || total >= projectScaffoldMaxTotalBytes {
			skipped = append(skipped, name+" (scaffold bounds)")
			continue
		}
		if hdr.Size > projectScaffoldMaxFileBytes {
			skipped = append(skipped, name+" (too large)")
			continue
		}
		content, err := io.ReadAll(io.LimitReader(tr, projectScaffoldMaxFileBytes+1))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s from %s: %w", name, archiveURL, err)
		}
		if len(content) > projectScaffoldMaxFileBytes {
			skipped = append(skipped, name+" (too large)")
			continue
		}
		if !utf8.Valid(content) {
			skipped = append(skipped, name+" (binary)")
			continue
		}
		total += len(content)
		files = append(files, projectScaffoldFile{Path: name, Content: string(content)})
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("archive %s contained no usable text files", archiveURL)
	}
	return files, skipped, nil
}

// materializeProjectScaffold writes the template's scaffold tree into the
// project workspace, records a pristine-content manifest, commits the tree to
// the project's git repository (the durable source of truth CI builds from),
// and kicks a development sync. It only acts on an EMPTY workspace: template
// switches on a project with existing files must never clobber user code
// (re-hydrate or edit instead). Returns nil when there is nothing to do (no
// scaffold declared, no workspace store, workspace not empty); a
// fetch/extract failure is reported in the result's Error, never as an error
// — binding a template must not fail because a scaffold host is unreachable.
// httpReq carries the caller's Authorization for the git commit.
func (s *Server) materializeProjectScaffold(ctx context.Context, id identity, p *aiv1alpha1.Project, info projectTemplateInfo, httpReq *http.Request) *projectScaffoldMaterialization {
	if info.Scaffold == nil || s.workspaces == nil || p == nil {
		return nil
	}
	scope := projectWorkspaceScope(id, p.Name)
	existing, err := s.workspaces.ListFiles(ctx, scope, workspace.ListOptions{Limit: 1})
	if err != nil {
		klog.V(1).Infof("scaffold: list workspace for project %s: %v", p.Name, err)
		return nil
	}
	if len(existing.Files) > 0 {
		return nil
	}

	result := &projectScaffoldMaterialization{
		Repository: info.Scaffold.Repository,
		Ref:        info.Scaffold.Ref,
	}
	files, skipped, err := fetchProjectScaffoldFiles(ctx, *info.Scaffold)
	if err != nil {
		klog.V(1).Infof("scaffold: fetch %s@%s for project %s: %v", info.Scaffold.Repository, info.Scaffold.Ref, p.Name, err)
		result.Error = err.Error()
		return result
	}
	result.Skipped = skipped
	manifest := projectScaffoldManifest{
		Repository: info.Scaffold.Repository,
		Ref:        info.Scaffold.Ref,
		Files:      make(map[string]string, len(files)),
	}
	for _, f := range files {
		if _, err := s.workspaces.WriteFile(ctx, scope, workspace.WriteOptions{Path: f.Path, Content: f.Content}); err != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("%s (workspace: %v)", f.Path, err))
			continue
		}
		result.Written = append(result.Written, f.Path)
		manifest.Files[f.Path] = projectSyncFileHash(f.Content)
	}
	if raw, err := json.MarshalIndent(manifest, "", "  "); err == nil {
		if _, err := s.workspaces.WriteFile(ctx, scope, workspace.WriteOptions{Path: projectScaffoldManifestPath, Content: string(raw) + "\n"}); err != nil {
			klog.V(1).Infof("scaffold: write manifest for project %s: %v", p.Name, err)
		}
	}
	s.commitProjectScaffold(ctx, id, p, files, result, httpReq)
	// Push the scaffold into the live development environment (best-effort;
	// the instance may still be provisioning — later syncs retry).
	go s.syncDevelopmentAfterMutation(id, p.DeepCopy(), "materialize_scaffold")
	return result
}

// commitProjectScaffold pushes the materialized scaffold tree into the
// project's git repository as one commit. Best-effort: a project without a
// repository yet (or a failed commit) keeps the workspace scaffold and
// reports the gap on the result — the tree reaches git with the next
// hydrate-aware commit or re-select.
func (s *Server) commitProjectScaffold(ctx context.Context, id identity, p *aiv1alpha1.Project, files []projectScaffoldFile, result *projectScaffoldMaterialization, httpReq *http.Request) {
	if httpReq == nil || len(files) == 0 {
		return
	}
	repoRef := projectLinkedRepositoryRef(p)
	if repoRef == "" || strings.TrimSpace(id.clusterID) == "" {
		return
	}
	payload := make([]map[string]string, 0, len(files))
	for _, f := range files {
		payload = append(payload, map[string]string{"path": f.Path, "content": f.Content})
	}
	commitArgs := map[string]any{
		"repositoryRef": repoRef,
		"message":       fmt.Sprintf("chore(app-studio): materialize template scaffold (%s@%s)", result.Repository, result.Ref),
		"files":         payload,
	}
	resp, err := callProjectMCPTool(ctx, s.mcpEndpoint(id.clusterID), httpReq, id.tenantPath, s.mcpInsecureSkipTLSVerify, projectToolCodeCommitFiles, commitArgs)
	if err != nil {
		result.CommitError = err.Error()
		return
	}
	if status := projectToolCallResultStatus(projectToolCodeCommitFiles, resp); status != "succeeded" {
		result.CommitError = "commit " + status
		return
	}
	result.Committed = true
}

// projectAssistantToolJSONResultWithTemplateWarning wraps a mutation tool's
// JSON result with a loud warning when the project has no development
// template. Without a template nothing can run — no sandbox, no toolchain
// contract, no runtime — so source written first is written blind: the model
// picks a random framework the platform may be unable to execute, exactly
// the failure the template scaffolds exist to prevent. The write still
// succeeds (the engine's initial-creation flow legitimately interleaves
// writes with binding), but every result carries the correction until a
// template is bound.
func projectAssistantToolJSONResultWithTemplateWarning(p *aiv1alpha1.Project, out any, err error) (string, error) {
	raw, err := projectAssistantToolJSONResult(out, err)
	if err != nil {
		return "", err
	}
	if p != nil && p.Spec.Template != nil && strings.TrimSpace(p.Spec.Template.Name) != "" {
		return raw, nil
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(raw), &decoded) != nil || decoded == nil {
		return raw, nil
	}
	decoded["warning"] = "NO DEVELOPMENT TEMPLATE IS BOUND: this source cannot run anywhere yet, and code written now may target a stack the platform cannot execute. Bind the template NOW — inspect_development_templates to compare candidates, then select_project_template — BEFORE writing more source; templates with a starter scaffold pre-populate the workspace with a working app to edit."
	wrapped, marshalErr := json.Marshal(decoded)
	if marshalErr != nil {
		return raw, nil
	}
	return string(wrapped), nil
}

// projectScaffoldFilePristine reports whether the workspace file at path
// still carries exactly the content the template scaffold materialized —
// i.e. replacing it wholesale loses nothing anyone wrote.
func (s *Server) projectScaffoldFilePristine(ctx context.Context, scope workspace.Scope, path string) bool {
	if s.workspaces == nil {
		return false
	}
	manifestRaw, err := s.workspaces.ReadFile(ctx, scope, workspace.ReadOptions{Path: projectScaffoldManifestPath})
	if err != nil || manifestRaw.Binary || manifestRaw.Truncated {
		return false
	}
	var manifest projectScaffoldManifest
	if err := json.Unmarshal([]byte(manifestRaw.Content), &manifest); err != nil {
		return false
	}
	clean, err := workspace.CleanProjectPath(path)
	if err != nil {
		return false
	}
	want, ok := manifest.Files[clean]
	if !ok {
		return false
	}
	current, err := s.workspaces.ReadFile(ctx, scope, workspace.ReadOptions{Path: clean})
	if err != nil || current.Binary || current.Truncated {
		return false
	}
	return projectSyncFileHash(current.Content) == want
}
