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
	"context"
	"fmt"

	"github.com/faroshq/provider-app-studio/workspace"
)

// projectAssistantApplyPatchesMaxEdits bounds a single batch edit so a runaway
// call cannot rewrite the whole project in one round-trip.
const projectAssistantApplyPatchesMaxEdits = 40

type projectAssistantBatchEditResult struct {
	Index        int    `json:"index"`
	Path         string `json:"path"`
	Status       string `json:"status"` // applied | failed
	Replacements int    `json:"replacements,omitempty"`
	Error        string `json:"error,omitempty"`
}

type projectAssistantBatchEditsResult struct {
	Applied int                               `json:"applied"`
	Failed  int                               `json:"failed"`
	Results []projectAssistantBatchEditResult `json:"results"`
	Summary string                            `json:"summary"`
}

// projectAssistantApplyPatchesCall applies an ordered list of exact-match
// patches. Each edit is independent (exact text replacement), so one failure
// does not corrupt the others: failures are reported per-edit and the batch
// continues, letting the model see exactly what applied and react. The whole
// batch is gated by the same write permission / plan-envelope path check as a
// single apply_patch, with every edit path required to be within the envelope.
func projectAssistantApplyPatchesCall(ctx context.Context, s *Server, req projectAssistantToolCallRequest) (string, error) {
	rawEdits, ok := req.Arguments["edits"].([]any)
	if !ok || len(rawEdits) == 0 {
		return "", fmt.Errorf("apply_patches requires a non-empty edits array")
	}
	if len(rawEdits) > projectAssistantApplyPatchesMaxEdits {
		return "", fmt.Errorf("apply_patches accepts at most %d edits per call; split the change into multiple batches", projectAssistantApplyPatchesMaxEdits)
	}
	result := projectAssistantBatchEditsResult{Results: make([]projectAssistantBatchEditResult, 0, len(rawEdits))}
	for i, raw := range rawEdits {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		edit, ok := raw.(map[string]any)
		if !ok {
			result.Failed++
			result.Results = append(result.Results, projectAssistantBatchEditResult{
				Index:  i,
				Status: "failed",
				Error:  "edit is not an object",
			})
			continue
		}
		path := projectToolString(edit["path"])
		oldText, _ := projectToolRawString(edit["oldText"])
		newText, _ := projectToolRawString(edit["newText"])
		opts := workspace.PatchOptions{
			Path:       path,
			OldText:    oldText,
			NewText:    newText,
			ReplaceAll: projectToolBool(edit["replaceAll"]),
		}
		if _, _, err := previewProjectWorkspacePatch(ctx, s.workspaces, req.WorkspaceScope, opts); err != nil {
			result.Failed++
			result.Results = append(result.Results, projectAssistantBatchEditResult{
				Index:  i,
				Path:   path,
				Status: "failed",
				Error:  truncateProjectToolInfo(err.Error()),
			})
			continue
		}
		mutation, err := s.workspaces.ApplyPatch(ctx, req.WorkspaceScope, opts)
		if err != nil {
			result.Failed++
			result.Results = append(result.Results, projectAssistantBatchEditResult{
				Index:  i,
				Path:   path,
				Status: "failed",
				Error:  truncateProjectToolInfo(err.Error()),
			})
			continue
		}
		result.Applied++
		result.Results = append(result.Results, projectAssistantBatchEditResult{
			Index:        i,
			Path:         mutation.Path,
			Status:       "applied",
			Replacements: mutation.Replacements,
		})
	}
	result.Summary = fmt.Sprintf("Applied %d of %d edit(s)", result.Applied, result.Applied+result.Failed)
	if result.Failed > 0 {
		result.Summary += fmt.Sprintf("; %d failed — review the per-edit errors and retry the failed edits.", result.Failed)
	}
	return projectAssistantToolJSONResult(result, nil)
}
