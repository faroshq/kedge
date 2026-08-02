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

// Undo-snapshot retention.
//
// Every assistant run that touches files stores the before and after content
// of each file it touched, so the run can be undone. Nothing removed those
// except deleting the project, so a long-lived project accumulated one
// snapshot per run forever on a volume shared by every tenant — and when it
// filled, workspace writes failed for all of them.
//
// Snapshots are only useful while their run is recent enough to plausibly undo,
// so they are swept on age. Sweeping is best-effort and never blocks a
// mutation: a snapshot that outlives its window costs disk, while one deleted
// too eagerly costs the user their undo.

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// SnapshotSweepResult reports what one retention pass removed.
type SnapshotSweepResult struct {
	// Removed counts deleted run snapshots.
	Removed int
	// Scanned counts run snapshots examined.
	Scanned int
	// Errors are per-snapshot failures. A failure to remove one snapshot must
	// not abort the sweep, or a single unreadable directory would pin the whole
	// volume forever.
	Errors []error
}

// SweepSnapshots removes run snapshots last modified before cutoff, across
// every project in the store.
func (s *FileStore) SweepSnapshots(ctx context.Context, cutoff time.Time) (SnapshotSweepResult, error) {
	var result SnapshotSweepResult
	if s == nil || s.root == "" {
		return result, errors.New("project workspace store is not configured")
	}
	root := filepath.Join(s.root, workspaceSnapshotDirectory)
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read workspace snapshot root: %w", err)
	}

	// Layout is <root>/.assistant-snapshots/<org>/<workspace>/<project>/<runID>.
	for _, org := range entries {
		if !org.IsDir() {
			continue
		}
		orgDir := filepath.Join(root, org.Name())
		workspaces, err := os.ReadDir(orgDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("read %s: %w", orgDir, err))
			continue
		}
		for _, ws := range workspaces {
			if !ws.IsDir() {
				continue
			}
			wsDir := filepath.Join(orgDir, ws.Name())
			projects, err := os.ReadDir(wsDir)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("read %s: %w", wsDir, err))
				continue
			}
			for _, project := range projects {
				if !project.IsDir() {
					continue
				}
				if err := ctx.Err(); err != nil {
					return result, err
				}
				s.sweepProjectSnapshots(filepath.Join(wsDir, project.Name()), cutoff, &result)
			}
		}
	}
	return result, nil
}

func (s *FileStore) sweepProjectSnapshots(projectDir string, cutoff time.Time, result *SnapshotSweepResult) {
	runs, err := os.ReadDir(projectDir)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("read %s: %w", projectDir, err))
		return
	}
	for _, run := range runs {
		if !run.IsDir() {
			// Not a run snapshot — the deletion tombstone file lives here too
			// and must survive, since it is the sandbox's only record of what
			// to remove.
			continue
		}
		result.Scanned++
		runDir := filepath.Join(projectDir, run.Name())
		info, err := run.Info()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("stat %s: %w", runDir, err))
			continue
		}
		// Entries are written as the run mutates files, so the directory's own
		// modification time tracks the run's last write.
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(runDir); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("remove %s: %w", runDir, err))
			continue
		}
		result.Removed++
	}
}
