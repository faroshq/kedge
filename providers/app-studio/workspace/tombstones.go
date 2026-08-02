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

// Deletion tombstones.
//
// The development sync ships the workspace's current files to the sandbox, so
// a file removed here would simply stop being sent — and keep running in the
// sandbox forever. The sandbox agent accepts an explicit deletePaths list, so
// deletions are recorded as tombstones and replayed on every sync until the
// path is written again.
//
// Tombstones are deliberately not cleared after a successful sync: a project
// can have several sandboxes (one per component, and a fresh one after a
// runner is recreated), and deleting an already-absent path is a no-op for the
// agent. They are dropped when a file reappears at the same path, which is
// what keeps the list bounded in practice.

package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// workspaceDeletedPathsFile holds one project's deletion tombstones. It lives
// beside the snapshots directory, under a reserved segment no workspace file
// can occupy.
const workspaceDeletedPathsFile = ".assistant-deleted.json"

// maxTrackedDeletedPaths bounds the tombstone list. Beyond this the oldest
// entries are dropped: their files are long gone from every live sandbox, and
// an unbounded list would be replayed on every sync forever.
const maxTrackedDeletedPaths = 512

type workspaceDeletedPaths struct {
	Paths []string `json:"paths"`
}

func (s *FileStore) deletedPathsFile(scope Scope) (string, error) {
	dir, err := s.snapshotProjectDir(scope)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, workspaceDeletedPathsFile), nil
}

func (s *FileStore) readDeletedPaths(scope Scope) ([]string, error) {
	file, err := s.deletedPathsFile(scope)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workspace deletions: %w", err)
	}
	var doc workspaceDeletedPaths
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A corrupt tombstone list must not wedge every future mutation and
		// sync for the project; the cost of dropping it is that an already
		// deleted file lingers in the sandbox until it is deleted again.
		return nil, nil
	}
	return doc.Paths, nil
}

func (s *FileStore) writeDeletedPaths(scope Scope, paths []string) error {
	file, err := s.deletedPathsFile(scope)
	if err != nil {
		return err
	}
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create workspace deletions directory: %w", err)
	}
	encoded, err := json.Marshal(workspaceDeletedPaths{Paths: paths})
	if err != nil {
		return fmt.Errorf("encode workspace deletions: %w", err)
	}
	if err := writeFileAtomically(dir, file, encoded, 0o600, false); err != nil {
		return fmt.Errorf("persist workspace deletions: %w", err)
	}
	return nil
}

// recordDeletedPath adds one canonical path to the project's tombstones.
func (s *FileStore) recordDeletedPath(scope Scope, clean string) error {
	existing, err := s.readDeletedPaths(scope)
	if err != nil {
		return err
	}
	for _, path := range existing {
		if path == clean {
			return nil
		}
	}
	next := append(existing, clean)
	if len(next) > maxTrackedDeletedPaths {
		next = next[len(next)-maxTrackedDeletedPaths:]
	}
	return s.writeDeletedPaths(scope, next)
}

// clearDeletedPaths drops tombstones for paths that exist again, so a file
// deleted and later recreated is not removed from the sandbox on the next sync.
func (s *FileStore) clearDeletedPaths(scope Scope, cleanPaths ...string) error {
	if len(cleanPaths) == 0 {
		return nil
	}
	existing, err := s.readDeletedPaths(scope)
	if err != nil || len(existing) == 0 {
		return err
	}
	drop := make(map[string]bool, len(cleanPaths))
	for _, path := range cleanPaths {
		drop[path] = true
	}
	next := existing[:0:0]
	for _, path := range existing {
		if !drop[path] {
			next = append(next, path)
		}
	}
	if len(next) == len(existing) {
		return nil
	}
	return s.writeDeletedPaths(scope, next)
}

// cleanedFilePaths canonicalizes a batch of file paths, skipping any the store
// would have rejected anyway.
func cleanedFilePaths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if clean, err := cleanProjectPath(f.Path); err == nil {
			out = append(out, clean)
		}
	}
	return out
}

// DeletedPaths returns the paths removed from this workspace that a sandbox
// may still be running, sorted for a deterministic sync payload.
func (s *FileStore) DeletedPaths(scope Scope) ([]string, error) {
	if s == nil {
		return nil, errors.New("project workspace store is not configured")
	}
	defer s.lockScope(scope)()

	paths, err := s.readDeletedPaths(scope)
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out, nil
}
