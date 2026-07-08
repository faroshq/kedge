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
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"k8s.io/klog/v2"
)

// projectAssistantTraceEnabled turns on structured per-model-call telemetry for
// the assistant harness: prompt/cached/completion/reasoning token counts per
// call and per-run cumulative totals. It exists so operators can measure
// prompt-cache hit rates and token spend, and debug turn behaviour, without
// standing up a full APM. Set APP_STUDIO_ASSISTANT_TRACE=1 (or true).
var projectAssistantTraceEnabled = projectAssistantEnvTruthy(os.Getenv("APP_STUDIO_ASSISTANT_TRACE"))

func projectAssistantEnvTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

// recordProjectAssistantModelUsage folds one model call's token usage into the
// run totals and, when tracing is enabled, logs the per-call and cumulative
// figures. CachedTokens surfaces prompt-cache effectiveness; ReasoningTokens
// surfaces thinking-mode spend on models that report it.
func recordProjectAssistantModelUsage(ctx context.Context, runState *projectEinoAssistantRunState, usage *einomodel.TokenUsage) {
	if runState == nil || usage == nil {
		return
	}
	cached := usage.PromptTokenDetails.CachedTokens
	reasoning := usage.CompletionTokensDetails.ReasoningTokens
	totals := runState.AddTokenUsage(usage.PromptTokens, cached, usage.CompletionTokens, reasoning)
	if !projectAssistantTraceEnabled {
		return
	}
	klog.FromContext(ctx).Info("app studio assistant model usage",
		"promptTokens", usage.PromptTokens,
		"cachedTokens", cached,
		"completionTokens", usage.CompletionTokens,
		"reasoningTokens", reasoning,
		"runModelCalls", totals.ModelCalls,
		"runPromptTokens", totals.PromptTokens,
		"runCachedTokens", totals.CachedTokens,
		"runCompletionTokens", totals.CompletionTokens,
		"runCacheHitRatio", projectAssistantCacheHitRatio(totals),
	)
}

// projectAssistantCacheHitRatio reports the fraction of prompt tokens served
// from cache across the run, as a percentage rounded to an integer.
func projectAssistantCacheHitRatio(totals projectAssistantTokenUsage) int {
	if totals.PromptTokens <= 0 {
		return 0
	}
	return int(float64(totals.CachedTokens) * 100 / float64(totals.PromptTokens))
}
