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
	"strings"
	"testing"
)

func TestEinoApprovePlanToolRejectsMissingAllowedOperations(t *testing.T) {
	runState := newProjectEinoAssistantRunState()
	tool := projectEinoAssistantTool{runState: runState}

	result := tool.invokeApprovedPlanTool(context.Background(), "call-plan", projectAssistantToolSpec{
		Name: projectToolRequestProjectPlanApproval,
		Risk: projectAssistantToolRiskPlan,
	}, map[string]any{
		"summary":            "Build dashboard",
		"steps":              []any{"Write app shell"},
		"targetPaths":        []any{"src/"},
		"acceptanceCriteria": []any{"src/App.tsx exists"},
	})

	if !strings.Contains(result, "allowedOperations is required") {
		t.Fatalf("result = %q, want allowedOperations validation error", result)
	}
	if plan := runState.ApprovedPlan(); plan != nil {
		t.Fatalf("approved plan = %#v, want nil after malformed approve_plan", plan)
	}
}

func TestEinoToolPassesSessionSnapshotToLocalTool(t *testing.T) {
	runState := newProjectEinoAssistantRunState()
	runState.SetSessionSnapshot(projectEinoAssistantSessionSnapshot{
		LastRuntimeDeployment: &projectEinoAssistantSessionRuntime{
			Status: "ready",
			URL:    "https://demo.apps.example.com",
		},
	})
	var got *projectEinoAssistantSessionSnapshot
	localTool := projectAssistantToolFunc{
		spec: projectAssistantToolSpec{
			Name: "capture_session_snapshot",
			Risk: projectAssistantToolRiskRead,
		},
		call: func(_ context.Context, req projectAssistantToolCallRequest) (string, error) {
			got = req.SessionSnapshot
			return `{"status":"captured"}`, nil
		},
	}
	tool := projectEinoAssistantTool{
		tool:     localTool,
		req:      projectAssistantRunRequest{},
		runState: runState,
	}

	if _, err := tool.invokeAllowedTool(context.Background(), "call-session", localTool.Spec(), nil); err != nil {
		t.Fatalf("invokeAllowedTool returned error: %v", err)
	}
	if got == nil || got.LastRuntimeDeployment == nil {
		t.Fatalf("session snapshot = %#v, want runtime deployment snapshot", got)
	}
	if got.LastRuntimeDeployment.URL != "https://demo.apps.example.com" {
		t.Fatalf("runtime URL = %q, want session snapshot URL", got.LastRuntimeDeployment.URL)
	}
	got.LastRuntimeDeployment.Status = "mutated"
	if runState.SessionSnapshot().LastRuntimeDeployment.Status != "ready" {
		t.Fatal("tool received mutable run-state session snapshot")
	}
}
