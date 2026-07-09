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
	"os"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
)

// Context editing clears stale tool results from the model input once the
// conversation grows past a threshold, replacing them with placeholders while
// keeping the most recent tool exchanges intact. It is the eino-native,
// provider-agnostic analogue of Anthropic's clear_tool_uses context editing and
// runs as a cheap, no-model-call pass layered *under* the summarization
// middleware: clearing fires first (at a lower threshold) so a tool-heavy
// scaffolding session sheds old file reads before paying for a full summary.
//
// It is opt-in (APP_STUDIO_CONTEXT_EDITING) because clearing tool results
// rewrites the model input, and its interaction with App Studio's permission
// interrupt / checkpoint / resume flow should be validated against a live model
// before it becomes the default. Collaboration tools that carry interrupt state
// are excluded from clearing.
const projectAssistantContextEditingEnv = "APP_STUDIO_CONTEXT_EDITING"

// projectAssistantContextEditingEnabled reports whether the reduction
// (context-editing) middleware should be attached.
func projectAssistantContextEditingEnabled() bool {
	return projectAssistantEnvTruthy(os.Getenv(projectAssistantContextEditingEnv))
}

const (
	// projectAssistantContextClearRetentionMessages is how many of the most
	// recent messages are never cleared, so the live working set (latest tool
	// results the model is actively reasoning over) stays intact.
	projectAssistantContextClearRetentionMessages = 8
	// projectAssistantContextClearFractionOfSummary sets the clear threshold as
	// a fraction of the summarization trigger, so clearing always fires before
	// the more expensive summarization pass.
	projectAssistantContextClearFractionOfSummary = 0.75
)

// projectAssistantContextClearTokens returns the token count at which stale tool
// results are cleared for the configured model, kept below the summarization
// trigger so the cheap pass runs first.
func projectAssistantContextClearTokens(model string) int64 {
	return int64(float64(projectAssistantSummaryContextTokens(model)) * projectAssistantContextClearFractionOfSummary)
}

// newProjectAssistantContextEditingMiddleware builds the reduction middleware in
// clear-only mode (no truncation, no offload backend): once the input exceeds
// the clear threshold, the oldest tool results are replaced with placeholders,
// retaining the most recent exchanges and never touching collaboration tools.
func newProjectAssistantContextEditingMiddleware(ctx context.Context, req projectAssistantRunRequest) (adk.ChatModelAgentMiddleware, error) {
	return reduction.New(ctx, &reduction.Config{
		SkipTruncation:            true,
		MaxTokensForClear:         projectAssistantContextClearTokens(req.LLM.Model),
		ClearRetentionSuffixLimit: projectAssistantContextClearRetentionMessages,
		ClearExcludeTools: []string{
			projectToolAskFollowUp,
			projectToolRequestProjectPlanApproval,
		},
	})
}
