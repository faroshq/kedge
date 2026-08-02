/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faroshq/provider-app-studio/workspace"
)

func scaffoldTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(content)),
		}); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func TestProjectScaffoldArchiveURLs(t *testing.T) {
	urls, err := projectScaffoldArchiveURLs(projectTemplateScaffold{
		Repository: "https://github.com/faroshq/kedge-scaffold-application",
		Ref:        "v0.1.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://codeload.github.com/faroshq/kedge-scaffold-application/tar.gz/refs/tags/v0.1.0"
	if len(urls) == 0 || urls[0] != want {
		t.Fatalf("urls[0] = %v, want %s", urls, want)
	}

	urls, err = projectScaffoldArchiveURLs(projectTemplateScaffold{
		Repository: "https://git.example.com/org/starter.git",
		Ref:        "v2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://git.example.com/org/starter/archive/v2.tar.gz"; len(urls) != 1 || urls[0] != want {
		t.Fatalf("urls = %v, want [%s]", urls, want)
	}

	if _, err := projectScaffoldArchiveURLs(projectTemplateScaffold{Repository: "git@github.com:x/y.git"}); err == nil {
		t.Fatal("expected an error for a non-https repository URL")
	}
}

func TestFetchProjectScaffoldArchive(t *testing.T) {
	archive := scaffoldTestArchive(t, map[string]string{
		"starter-v0.1.0/web/package.json":             `{"name":"web"}`,
		"starter-v0.1.0/api/server.mjs":               "console.log('api')",
		"starter-v0.1.0/AGENTS.md":                    "# contract",
		"starter-v0.1.0/README.md":                    "scaffold repo readme",
		"starter-v0.1.0/LICENSE":                      "Apache",
		"starter-v0.1.0/.github/workflows/build.yaml": "name: Build",
		"starter-v0.1.0/web/logo.png":                 "\x89PNG\x00binary",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	files, skipped, err := fetchProjectScaffoldArchive(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Content
	}
	for _, want := range []string{"web/package.json", "api/server.mjs", "AGENTS.md"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected %s to be extracted; got %v", want, files)
		}
	}
	for _, banned := range []string{"README.md", "LICENSE", ".github/workflows/build.yaml"} {
		if _, ok := got[banned]; ok {
			t.Errorf("expected %s to be excluded from materialization", banned)
		}
	}
	foundBinarySkip := false
	for _, s := range skipped {
		if strings.HasPrefix(s, "web/logo.png") {
			foundBinarySkip = true
		}
	}
	if !foundBinarySkip {
		t.Errorf("expected the binary file to be reported skipped; skipped = %v", skipped)
	}
}

func TestProjectScaffoldFilePristine(t *testing.T) {
	s := &Server{workspaces: workspace.NewFileStore(t.TempDir())}
	scope := workspace.Scope{OrgUUID: "org", WorkspaceUUID: "ws", ProjectName: "proj"}
	ctx := context.Background()

	content := "export default 1\n"
	if _, err := s.workspaces.WriteFile(ctx, scope, workspace.WriteOptions{Path: "src/main.js", Content: content}); err != nil {
		t.Fatalf("write file: %v", err)
	}
	manifest := projectScaffoldManifest{
		Repository: "https://github.com/faroshq/kedge-scaffold-simple-webapp",
		Ref:        "v0.1.1",
		Files:      map[string]string{"src/main.js": projectSyncFileHash(content)},
	}
	raw, _ := json.Marshal(manifest)
	if _, err := s.workspaces.WriteFile(ctx, scope, workspace.WriteOptions{Path: projectScaffoldManifestPath, Content: string(raw)}); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if !s.projectScaffoldFilePristine(ctx, scope, "src/main.js") {
		t.Fatal("unedited scaffold file should be pristine")
	}
	if s.projectScaffoldFilePristine(ctx, scope, "index.html") {
		t.Fatal("a path missing from the manifest must not be pristine")
	}
	if _, err := s.workspaces.WriteFile(ctx, scope, workspace.WriteOptions{Path: "src/main.js", Content: "edited\n"}); err != nil {
		t.Fatalf("edit file: %v", err)
	}
	if s.projectScaffoldFilePristine(ctx, scope, "src/main.js") {
		t.Fatal("an edited scaffold file must not be pristine")
	}
}

func TestFetchProjectScaffoldArchiveEmpty(t *testing.T) {
	archive := scaffoldTestArchive(t, map[string]string{
		"starter/LICENSE": "Apache",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	if _, _, err := fetchProjectScaffoldArchive(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for an archive with no usable files")
	}
}
