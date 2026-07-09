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
	"strings"

	"github.com/cloudwego/eino-examples/adk/common/tool/graphtool"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

// verify_project closes the edit → error → fix loop. The SandboxRunner data
// plane exposes no dedicated build-exec verb, so verification reads the live dev
// process output: it pulls recent runtime logs, classifies whether they show a
// build/compile/crash error, and returns a structured pass/fail the model can
// react to instead of committing blind and hoping.

const (
	// projectAssistantVerifyLogTail bounds how many trailing log lines the
	// verifier inspects for error signatures.
	projectAssistantVerifyLogTail = 200
	// projectAssistantVerifyMaxErrors bounds how many matched error lines are
	// returned so a crash-loop cannot flood the model context.
	projectAssistantVerifyMaxErrors = 20
)

type projectAssistantVerifyToolInput struct {
	TailLines int `json:"tailLines,omitempty" jsonschema_description:"Maximum number of trailing log lines to inspect for build/compile/runtime errors (default 200, max 500)."`
}

type projectAssistantVerifyResult struct {
	// Status is one of: passing, failing, unavailable.
	Status    string   `json:"status"`
	Summary   string   `json:"summary"`
	Errors    []string `json:"errors,omitempty"`
	Lines     []string `json:"lines,omitempty"`
	Blockers  []string `json:"blockers,omitempty"`
	NextSteps []string `json:"nextSteps,omitempty"`
}

func newProjectAssistantVerifyGraphTool(runCtx projectAssistantWorkflowRunContext) (einotool.BaseTool, error) {
	workflow := compose.NewWorkflow[*projectAssistantVerifyToolInput, *projectAssistantVerifyResult]()
	workflow.AddLambdaNode("verify-project", compose.InvokableLambda(verifyProjectAssistantRuntime(runCtx))).
		AddInput(compose.START)
	workflow.End().AddInput("verify-project")
	return graphtool.NewInvokableGraphTool(
		workflow,
		projectToolVerifyProject,
		"Verify the app after edits: inspect the live development runtime and its recent logs for build, compile, or crash errors, and report whether the app is serving cleanly or failing. Call this after applying workspace edits and before telling the user the change is done.",
		compose.WithGraphName("app-studio-verify-project"),
	)
}

func verifyProjectAssistantRuntime(runCtx projectAssistantWorkflowRunContext) func(context.Context, *projectAssistantVerifyToolInput) (*projectAssistantVerifyResult, error) {
	return func(ctx context.Context, args *projectAssistantVerifyToolInput) (*projectAssistantVerifyResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tail := projectAssistantVerifyLogTail
		if args != nil && args.TailLines > 0 {
			tail = args.TailLines
		}
		if tail > projectAssistantRuntimeLogsMaxTail {
			tail = projectAssistantRuntimeLogsMaxTail
		}
		server, id, target, blocked := projectAssistantRuntimeCallContext(ctx, runCtx)
		if blocked != nil {
			// Nothing runs (no template bound, sandbox not ready, or no runtime
			// client). Report honestly rather than claiming a passing build.
			return &projectAssistantVerifyResult{
				Status:    "unavailable",
				Summary:   "Cannot verify yet: " + blocked.Summary,
				Blockers:  blocked.Blockers,
				NextSteps: blocked.NextSteps,
			}, nil
		}
		component := ""
		if len(target.Components) > 0 {
			component = target.sortedComponents()[0]
		}
		body, status, err := server.dataPlaneGet(ctx, id, target.dataPlaneRefFor(component), dataPlaneVerbLog, projectAssistantRuntimeLogsMaxBytes)
		if err != nil {
			return &projectAssistantVerifyResult{
				Status:  "unavailable",
				Summary: "Cannot verify: runtime logs are temporarily unavailable: " + err.Error(),
			}, nil
		}
		if status < 200 || status >= 300 {
			return &projectAssistantVerifyResult{
				Status:  "unavailable",
				Summary: fmt.Sprintf("Cannot verify: runtime logs are unavailable (status %d).", status),
			}, nil
		}
		lines := boundedRuntimeLogLines(string(body), tail)
		if len(lines) == 0 {
			return &projectAssistantVerifyResult{
				Status:  "unavailable",
				Summary: "The runtime has not produced any logs yet; the dev process may still be starting. Wait and verify again.",
			}, nil
		}
		errorsFound := projectAssistantDetectRuntimeErrors(lines)
		if len(errorsFound) == 0 {
			return &projectAssistantVerifyResult{
				Status:  "passing",
				Summary: "No build, compile, or crash errors detected in the recent development runtime logs.",
				Lines:   projectAssistantVerifyTailLines(lines),
			}, nil
		}
		return &projectAssistantVerifyResult{
			Status:  "failing",
			Summary: fmt.Sprintf("Detected %d error line(s) in the development runtime logs; the app is not building or serving cleanly.", len(errorsFound)),
			Errors:  errorsFound,
			NextSteps: []string{
				"Read the error lines above, locate the offending file with search_project_files or read_project_file, and apply a targeted fix.",
				"After fixing, verify again to confirm the runtime is serving cleanly.",
			},
		}, nil
	}
}

// projectAssistantVerifyTailLines returns a bounded slice of recent log lines
// for a passing result so the model sees the serving evidence without the full
// buffer.
func projectAssistantVerifyTailLines(lines []string) []string {
	const keep = 12
	if len(lines) <= keep {
		return lines
	}
	return lines[len(lines)-keep:]
}

// projectAssistantRuntimeErrorSignatures are substrings (lowercased) that mark a
// build, compile, or crash failure across the language/toolchain families App
// Studio templates use (Node/TS, Python, Go, and generic process failures).
var projectAssistantRuntimeErrorSignatures = []string{
	"error:",
	"failed to compile",
	"build failed",
	"compilation error",
	"syntaxerror",
	"typeerror",
	"referenceerror",
	"cannot find module",
	"module not found",
	"cannot resolve",
	"unexpected token",
	"npm err!",
	"pnpm err",
	"yarn error",
	"panic:",
	"traceback (most recent call last)",
	"modulenotfounderror",
	"importerror",
	"exception in",
	"unhandled exception",
	"unhandledrejection",
	"segmentation fault",
	"address already in use",
	"eaddrinuse",
	"econnrefused",
	"exited with code 1",
	"exited with code",
	"non-zero exit",
	"fatal error",
	"cannot start",
	"crash",
}

// projectAssistantRuntimeErrorExclusions filter out lines that contain an error
// signature only incidentally (log-level config, healthy request logs), to keep
// false positives from tripping the verifier.
var projectAssistantRuntimeErrorExclusions = []string{
	"log_level",
	"loglevel",
	"error_reporting",
	"no error",
	"0 errors",
	"errorhandler registered",
	"error boundary",
}

// projectAssistantDetectRuntimeErrors scans log lines for build/compile/crash
// error signatures and returns the matched lines (bounded), most recent last.
func projectAssistantDetectRuntimeErrors(lines []string) []string {
	var matched []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if projectAssistantRuntimeErrorLineExcluded(lower) {
			continue
		}
		if projectAssistantRuntimeErrorLineMatches(lower) {
			matched = append(matched, truncateProjectToolInfo(trimmed))
		}
	}
	if len(matched) > projectAssistantVerifyMaxErrors {
		matched = matched[len(matched)-projectAssistantVerifyMaxErrors:]
	}
	return matched
}

func projectAssistantRuntimeErrorLineMatches(lower string) bool {
	for _, sig := range projectAssistantRuntimeErrorSignatures {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

func projectAssistantRuntimeErrorLineExcluded(lower string) bool {
	for _, ex := range projectAssistantRuntimeErrorExclusions {
		if strings.Contains(lower, ex) {
			return true
		}
	}
	return false
}
