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
	"errors"
	"strings"
	"testing"
	"time"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/store"
)

func TestProjectAssistantSummaryContextTokensScalesWithModel(t *testing.T) {
	cases := []struct {
		model   string
		wantMin int
	}{
		{"claude-sonnet-5", 100000},   // 200k window * 0.6
		{"gpt-5.4", 240000},           // 400k window * 0.6
		{"gemini-2.5-flash", 300000},  // 1M window * 0.6, capped at 300k
		{"some-unknown-model", 24000}, // default window keeps >= floor
	}
	for _, tc := range cases {
		got := projectAssistantSummaryContextTokens(tc.model)
		if got < tc.wantMin {
			t.Errorf("summary trigger for %q = %d, want >= %d", tc.model, got, tc.wantMin)
		}
		if got > projectAssistantSummaryContextTokenCap {
			t.Errorf("summary trigger for %q = %d exceeds cap %d", tc.model, got, projectAssistantSummaryContextTokenCap)
		}
		if got < projectAssistantSummaryContextTokenFloor {
			t.Errorf("summary trigger for %q = %d below floor %d", tc.model, got, projectAssistantSummaryContextTokenFloor)
		}
	}
}

func TestProjectEinoRetryableError(t *testing.T) {
	retryable := []error{
		errors.New("received status 429 Too Many Requests"),
		errors.New("Error: overloaded_error"),
		errors.New("502 Bad Gateway"),
		errors.New("connection reset by peer"),
		errors.New("context deadline exceeded during dial"),
	}
	for _, err := range retryable {
		if !projectEinoRetryableError(err) {
			t.Errorf("expected retryable: %v", err)
		}
	}
	notRetryable := []error{
		nil,
		errors.New("invalid api key"),
		errors.New("model not found"),
		context.Canceled,
		context.DeadlineExceeded,
	}
	for _, err := range notRetryable {
		if projectEinoRetryableError(err) {
			t.Errorf("expected non-retryable: %v", err)
		}
	}
}

func TestProjectEinoRetryDoStopsOnSuccess(t *testing.T) {
	calls := 0
	err := projectEinoRetryDo(context.Background(), 3, time.Millisecond, func() error {
		calls++
		if calls < 2 {
			return errors.New("503 service unavailable")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestProjectEinoRetryDoDoesNotRetryPermanent(t *testing.T) {
	calls := 0
	err := projectEinoRetryDo(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return errors.New("invalid request: bad model")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on permanent error)", calls)
	}
}

func TestProjectAssistantDetectRuntimeErrors(t *testing.T) {
	failing := []string{
		"web  | ready in 320ms",
		"web  | ERROR: Cannot find module './missing'",
		"web  | SyntaxError: Unexpected token '<'",
	}
	got := projectAssistantDetectRuntimeErrors(failing)
	if len(got) < 2 {
		t.Fatalf("expected >= 2 error lines, got %d: %v", len(got), got)
	}

	healthy := []string{
		"web  | ready in 210ms",
		"web  | GET / 200 in 5ms",
		"config | log_level=error",
	}
	if got := projectAssistantDetectRuntimeErrors(healthy); len(got) != 0 {
		t.Fatalf("expected no errors on healthy logs, got %v", got)
	}
}

func TestProjectAssistantWriteTargetPathsBatch(t *testing.T) {
	args := map[string]any{
		"edits": []any{
			map[string]any{"path": "src/a.ts"},
			map[string]any{"path": "./src/b.ts"},
			map[string]any{"path": ""},
		},
	}
	paths := projectAssistantWriteTargetPaths(projectToolApplyPatches, args)
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want 2 non-empty normalized paths", paths)
	}
}

func TestProjectAssistantApprovedPlanAllowsBatchWithinEnvelope(t *testing.T) {
	plan := &projectAssistantApprovedPlan{
		Operations:  []string{projectToolApplyPatch},
		TargetPaths: []string{"src/"},
	}
	within := map[string]any{
		"edits": []any{
			map[string]any{"path": "src/a.ts"},
			map[string]any{"path": "src/nested/b.ts"},
		},
	}
	if !projectAssistantApprovedPlanAllowsWrite(plan, projectToolApplyPatches, within) {
		t.Fatal("batch fully within envelope should be allowed under an apply_patch grant")
	}
	outside := map[string]any{
		"edits": []any{
			map[string]any{"path": "src/a.ts"},
			map[string]any{"path": "secrets/token.txt"},
		},
	}
	if projectAssistantApprovedPlanAllowsWrite(plan, projectToolApplyPatches, outside) {
		t.Fatal("batch with an out-of-envelope path must not be auto-allowed")
	}
}

func TestProjectAssistantTokenUsageAccumulates(t *testing.T) {
	runState := newProjectEinoAssistantRunState()
	runState.AddTokenUsage(1000, 800, 200, 50)
	totals := runState.AddTokenUsage(500, 400, 100, 0)
	if totals.ModelCalls != 2 {
		t.Fatalf("model calls = %d, want 2", totals.ModelCalls)
	}
	if totals.PromptTokens != 1500 || totals.CachedTokens != 1200 || totals.CompletionTokens != 300 {
		t.Fatalf("unexpected totals: %+v", totals)
	}
	if got := projectAssistantCacheHitRatio(totals); got != 80 {
		t.Fatalf("cache hit ratio = %d, want 80", got)
	}
	if got := projectAssistantCacheHitRatio(projectAssistantTokenUsage{}); got != 0 {
		t.Fatalf("empty cache hit ratio = %d, want 0", got)
	}
}

func TestProjectAssistantEnvTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on", "enabled"} {
		if !projectAssistantEnvTruthy(v) {
			t.Errorf("%q should be truthy", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no", "off", "nope"} {
		if projectAssistantEnvTruthy(v) {
			t.Errorf("%q should be falsy", v)
		}
	}
}

func TestProjectAssistantHistoryToolEvidence(t *testing.T) {
	history := []store.Message{
		{Role: aiv1alpha1.ProjectMessageRoleUser, Content: "add a button"},
		{
			Role:    aiv1alpha1.ProjectMessageRoleAssistant,
			Content: "done",
			Metadata: map[string]any{
				projectMessageMetadataAssistantActions: []projectAssistantUIAction{
					{Tool: "read_project_file", Arguments: "src/App.tsx", Detail: "read 40 lines", Status: "succeeded"},
					{Tool: "apply_patch", Arguments: "src/App.tsx", Status: "succeeded"},
				},
			},
		},
	}
	got := projectAssistantHistoryToolEvidence(history)
	if got == "" {
		t.Fatal("expected non-empty evidence block")
	}
	for _, want := range []string{"read_project_file", "src/App.tsx", "apply_patch"} {
		if !strings.Contains(got, want) {
			t.Errorf("evidence missing %q; got:\n%s", want, got)
		}
	}

	if projectAssistantHistoryToolEvidence(nil) != "" {
		t.Error("nil history should yield empty evidence")
	}
}
