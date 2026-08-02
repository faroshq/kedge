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

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepSnapshotsRemovesOnlyExpiredRuns(t *testing.T) {
	store := NewFileStore(t.TempDir())
	scope := Scope{OrgUUID: "org-a", WorkspaceUUID: "ws-1", ProjectName: "demo"}
	ctx := context.Background()

	if err := store.ApplyFiles(ctx, scope, []File{{Path: "a.txt", Content: "one\n"}}); err != nil {
		t.Fatalf("ApplyFiles returned error: %v", err)
	}
	for _, run := range []string{"run-old", "run-new"} {
		if _, err := store.WriteFile(ctx, scope, WriteOptions{
			Path:       "a.txt",
			Content:    "changed by " + run + "\n",
			SnapshotID: run,
		}); err != nil {
			t.Fatalf("WriteFile(%s) returned error: %v", run, err)
		}
	}

	// Age one run past the window.
	oldDir, err := store.snapshotDir(scope, "run-old")
	if err != nil {
		t.Fatalf("snapshotDir returned error: %v", err)
	}
	stale := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldDir, stale, stale); err != nil {
		t.Fatalf("Chtimes returned error: %v", err)
	}

	result, err := store.SweepSnapshots(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SweepSnapshots returned error: %v", err)
	}
	if result.Removed != 1 {
		t.Fatalf("removed = %d, want 1 (only the expired run)", result.Removed)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("sweep errors = %v", result.Errors)
	}

	// The expired run is no longer undoable; the recent one still is.
	if _, err := store.RestoreSnapshot(ctx, scope, "run-old"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("restore of swept run = %v, want ErrSnapshotNotFound", err)
	}
	if _, err := store.RestoreSnapshot(ctx, scope, "run-new"); err != nil {
		t.Fatalf("restore of retained run returned error: %v", err)
	}
}

// TestSweepSnapshotsPreservesDeletionTombstones guards the sandbox's only
// record of which files to remove: it lives beside the run snapshots, and
// deleting it would resurrect deleted files in the sandbox on the next sync.
func TestSweepSnapshotsPreservesDeletionTombstones(t *testing.T) {
	store := NewFileStore(t.TempDir())
	scope := Scope{OrgUUID: "org-a", WorkspaceUUID: "ws-1", ProjectName: "demo"}
	ctx := context.Background()

	if err := store.ApplyFiles(ctx, scope, []File{{Path: "gone.txt", Content: "bye\n"}}); err != nil {
		t.Fatalf("ApplyFiles returned error: %v", err)
	}
	if _, err := store.DeleteFile(ctx, scope, DeleteOptions{Path: "gone.txt"}); err != nil {
		t.Fatalf("DeleteFile returned error: %v", err)
	}

	// Age everything, then sweep with a cutoff that would expire any run.
	projectDir, err := store.snapshotProjectDir(scope)
	if err != nil {
		t.Fatalf("snapshotProjectDir returned error: %v", err)
	}
	stale := time.Now().Add(-96 * time.Hour)
	_ = filepath.Walk(projectDir, func(path string, _ os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chtimes(path, stale, stale)
		}
		return nil
	})
	if _, err := store.SweepSnapshots(ctx, time.Now()); err != nil {
		t.Fatalf("SweepSnapshots returned error: %v", err)
	}

	deleted, err := store.DeletedPaths(scope)
	if err != nil {
		t.Fatalf("DeletedPaths returned error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "gone.txt" {
		t.Fatalf("tombstones after sweep = %v, want [gone.txt]", deleted)
	}
}

func TestSweepSnapshotsHandlesMissingRoot(t *testing.T) {
	store := NewFileStore(t.TempDir())
	result, err := store.SweepSnapshots(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("SweepSnapshots on an empty store returned error: %v", err)
	}
	if result.Removed != 0 || result.Scanned != 0 {
		t.Fatalf("result = %#v, want an empty sweep", result)
	}
}
