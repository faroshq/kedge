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
	"strings"
	"testing"
)

func TestProjectRuntimeIssuesClassifyCommonFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		line        string
		wantKind    projectRuntimeIssueKind
		wantSubject string
	}{
		{
			name:        "npm missing module",
			line:        "Error: Cannot find module 'express'",
			wantKind:    projectRuntimeIssueMissingModule,
			wantSubject: "express",
		},
		{
			name:        "webpack module not found",
			line:        "Module not found: Error: Can't resolve 'react-router-dom' in '/app/src'",
			wantKind:    projectRuntimeIssueMissingModule,
			wantSubject: "react-router-dom",
		},
		{
			name:        "vite failed import",
			line:        "Failed to resolve import \"./components/Header\" from \"src/App.tsx\"",
			wantKind:    projectRuntimeIssueMissingModule,
			wantSubject: "./components/Header",
		},
		{
			name:        "missing script",
			line:        "npm error Missing script: \"dev\"",
			wantKind:    projectRuntimeIssueMissingScript,
			wantSubject: "dev",
		},
		{
			name:     "syntax error",
			line:     "SyntaxError: Unexpected token '}'",
			wantKind: projectRuntimeIssueSyntax,
		},
		{
			name:     "compile failure",
			line:     "Failed to compile.",
			wantKind: projectRuntimeIssueCompile,
		},
		{
			name:     "port in use",
			line:     "Error: listen EADDRINUSE: address already in use :::3000",
			wantKind: projectRuntimeIssuePortInUse,
		},
		{
			name:     "permission denied",
			line:     "Error: EACCES: permission denied, open '/etc/hosts'",
			wantKind: projectRuntimeIssuePermission,
		},
		{
			name:     "process crash",
			line:     "process exited: signal: killed",
			wantKind: projectRuntimeIssueCrash,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := projectRuntimeIssuesFromLogs("", []string{tc.line})
			if len(issues) != 1 {
				t.Fatalf("issues = %#v, want exactly one", issues)
			}
			if issues[0].Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", issues[0].Kind, tc.wantKind)
			}
			if tc.wantSubject != "" && issues[0].Subject != tc.wantSubject {
				t.Fatalf("subject = %q, want %q", issues[0].Subject, tc.wantSubject)
			}
			if strings.TrimSpace(issues[0].Remediation) == "" {
				t.Fatal("issue carries no remediation; the model would have to derive the fix")
			}
		})
	}
}

func TestProjectRuntimeIssuesIgnoreHealthyOutput(t *testing.T) {
	lines := []string{
		"VITE v6.0.0  ready in 412 ms",
		"➜  Local:   http://localhost:3000/",
		"GET / 200 in 12ms",
		"[kedge reload] npm run dev",
	}
	if issues := projectRuntimeIssuesFromLogs("web", lines); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none for healthy output", issues)
	}
}

// TestProjectRuntimeIssuesDeduplicateRepeatedFaults covers the dominant real
// shape of a broken dev server: the same fault reprinted on every reload.
func TestProjectRuntimeIssuesDeduplicateRepeatedFaults(t *testing.T) {
	lines := []string{
		"Error: Cannot find module 'express'",
		"    at Module._resolveFilename (node:internal/modules/cjs/loader:1145:15)",
		"Error: Cannot find module 'express'",
		"Error: Cannot find module 'express'",
	}
	issues := projectRuntimeIssuesFromLogs("api", lines)
	if len(issues) != 1 {
		t.Fatalf("issues = %#v, want one deduplicated issue", issues)
	}
	if issues[0].Occurrences != 3 {
		t.Fatalf("occurrences = %d, want 3", issues[0].Occurrences)
	}
	if issues[0].Component != "api" {
		t.Fatalf("component = %q, want api", issues[0].Component)
	}
}

func TestProjectRuntimeIssueLocatesWorkspaceFile(t *testing.T) {
	issues := projectRuntimeIssuesFromLogs("", []string{
		"SyntaxError: Unexpected token '}' at /app/src/App.tsx:42:7",
	})
	if len(issues) != 1 {
		t.Fatalf("issues = %#v, want one", issues)
	}
	// The sandbox path prefix is stripped so the path matches what the
	// assistant's file tools accept.
	if issues[0].File != "src/App.tsx" || issues[0].Line != 42 {
		t.Fatalf("location = %q:%d, want src/App.tsx:42", issues[0].File, issues[0].Line)
	}
}

// TestProjectRuntimeIssueIgnoresDependencyStackFrames keeps the assistant from
// being pointed at code it does not own and cannot edit.
func TestProjectRuntimeIssueIgnoresDependencyStackFrames(t *testing.T) {
	issues := projectRuntimeIssuesFromLogs("", []string{
		"SyntaxError: boom at /app/node_modules/react/index.js:10:1",
	})
	if len(issues) != 1 {
		t.Fatalf("issues = %#v, want one", issues)
	}
	if issues[0].File != "" {
		t.Fatalf("file = %q, want no location for a dependency frame", issues[0].File)
	}
}

// TestProjectRuntimeIssueRoutingSeparatesOperationalFromSourceFaults pins the
// cheap rung of the fix ladder: only a diagnosed source defect should send the
// assistant to edit code.
func TestProjectRuntimeIssueRoutingSeparatesOperationalFromSourceFaults(t *testing.T) {
	sourceFault := projectAssistantRuntimeVerificationResult{
		Status: "not_ready",
		Logs: &projectAssistantRuntimeLogsResult{
			Blockers: []string{"cannot find module"},
			Issues:   projectRuntimeIssuesFromLogs("", []string{"Error: Cannot find module 'express'"}),
		},
	}
	sourceFault.Issues = sourceFault.Logs.Issues
	if got := projectEinoAssistantRuntimeVerificationDisposition(sourceFault); got != projectEinoAssistantVerificationRepair {
		t.Fatalf("disposition = %q for a missing module, want repair", got)
	}

	operationalFault := projectAssistantRuntimeVerificationResult{
		Status: "not_ready",
		Logs: &projectAssistantRuntimeLogsResult{
			Blockers: []string{"address already in use"},
			Issues:   projectRuntimeIssuesFromLogs("", []string{"Error: listen EADDRINUSE: address already in use :::3000"}),
		},
	}
	operationalFault.Issues = operationalFault.Logs.Issues
	if got := projectEinoAssistantRuntimeVerificationDisposition(operationalFault); got != projectEinoAssistantVerificationOperational {
		t.Fatalf("disposition = %q for a port conflict, want operational — editing source cannot fix a bound port", got)
	}

	// An undiagnosed blocker keeps the previous behaviour rather than being
	// silently downgraded out of the repair lane.
	unknownFault := projectAssistantRuntimeVerificationResult{
		Status: "not_ready",
		Logs:   &projectAssistantRuntimeLogsResult{Blockers: []string{"something went wrong"}},
	}
	if got := projectEinoAssistantRuntimeVerificationDisposition(unknownFault); got != projectEinoAssistantVerificationRepair {
		t.Fatalf("disposition = %q for an unclassified blocker, want repair", got)
	}
}

func TestProjectRuntimeIssueDiagnosisSummaryNamesTheFault(t *testing.T) {
	issues := projectRuntimeIssuesFromLogs("api", []string{"Error: Cannot find module 'express'"})
	summary := projectRuntimeIssueDiagnosisSummary(issues)
	if !strings.Contains(summary, "express") {
		t.Fatalf("summary = %q, want the unresolved module named", summary)
	}
	// The assistant's own instructions must NOT reach this summary: it is
	// rendered to the user, who cannot edit package.json from here.
	if strings.Contains(summary, "package.json") {
		t.Fatalf("summary = %q, leaks the assistant's remediation to the user", summary)
	}
}

// TestRuntimeIssueTextSeparatesUserFromAssistant pins the audience split. The
// user-facing blocker list once rendered the model's instructions verbatim, so
// a non-technical builder was told to "read the lines above the exit for the
// cause, fix it, then restart the runtime" — none of which they can do.
func TestRuntimeIssueTextSeparatesUserFromAssistant(t *testing.T) {
	issues := projectRuntimeIssuesFromLogs("backend", []string{
		"Failed to initialize API Error: The server does not support SSL connections",
		"process exited: exit status 1",
	})
	if len(issues) == 0 {
		t.Fatal("no issues classified")
	}

	// The assistant still gets actionable instructions.
	var sawRemediation bool
	for _, issue := range issues {
		if strings.TrimSpace(issue.Remediation) != "" {
			sawRemediation = true
		}
	}
	if !sawRemediation {
		t.Fatal("issues carry no remediation; the assistant would have to derive the fix")
	}

	// The user gets a description of the fault, with no instruction to operate
	// machinery they have no access to.
	for _, summary := range projectRuntimeIssueSummaries(issues) {
		lower := strings.ToLower(summary)
		for _, forbidden := range []string{
			"restart the runtime",
			"read the lines above",
			"re-sync",
			"apply_patch",
		} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("user-facing blocker %q instructs the user to %q", summary, forbidden)
			}
		}
	}

	// And it still says what actually broke.
	joined := strings.ToLower(strings.Join(projectRuntimeIssueSummaries(issues), " | "))
	if !strings.Contains(joined, "database") {
		t.Fatalf("user-facing blockers = %q, want the database failure named", joined)
	}
}

// TestProjectRuntimeIssueDiagnosesBackingServiceFailure covers the failure that
// this classifier originally missed in production: an app that cannot reach the
// database the template provisioned. Without a matcher it fell through to the
// generic crash entry, whose remediation ("read the lines above the exit") is
// precisely what the model had already failed to do.
func TestProjectRuntimeIssueDiagnosesBackingServiceFailure(t *testing.T) {
	for _, line := range []string{
		"Failed to initialize API Error: The server does not support SSL connections",
		"Error: connect ECONNREFUSED 10.96.165.135:5432",
		"error: password authentication failed for user \"appuser\"",
		"Error: getaddrinfo ENOTFOUND builder-job-swipe-dev-db",
	} {
		issues := projectRuntimeIssuesFromLogs("backend", []string{line})
		if len(issues) != 1 || issues[0].Kind != projectRuntimeIssueBackingService {
			t.Fatalf("issues for %q = %#v, want one backing_service issue", line, issues)
		}
		// The connection string is injected by the template, so pointing the
		// model at it wastes a turn; the app's own client options are the fault.
		if !strings.Contains(issues[0].Remediation, "Do not change the connection string") {
			t.Fatalf("remediation = %q, want it to protect the injected connection string", issues[0].Remediation)
		}
	}
}

// TestProjectRuntimeIssuesPreferTheCurrentFault pins the ordering defect that
// sent repair after a phantom: a dev server restarts repeatedly, so the log
// window holds faults it already recovered from. The one it is failing on NOW
// must rank first.
func TestProjectRuntimeIssuesPreferTheCurrentFault(t *testing.T) {
	lines := []string{
		// Older, already resolved — the file was synced after this.
		"Error: Cannot find module '/workspace/src/index.js'",
		"process exited: exit status 254",
		// What it is actually failing on now, printed on every restart since.
		"Failed to initialize API Error: The server does not support SSL connections",
		"Failed to initialize API Error: The server does not support SSL connections",
	}
	issues := projectRuntimeIssuesFromLogs("backend", lines)
	if len(issues) == 0 {
		t.Fatal("no issues classified")
	}
	if issues[0].Kind != projectRuntimeIssueBackingService {
		t.Fatalf("first issue = %q, want the current backing_service failure rather than the stale one", issues[0].Kind)
	}
}

// TestSourceDirectedRemediationsReachTheRepairLane pins the invariant that was
// violated in production: an issue whose remediation tells the assistant to
// change code must be classified source-fixable, or verification routes it to
// the read-only operational lane and the assistant is structurally unable to
// carry out its own instruction. The run then ends by asking the user to fix
// it, which is the one outcome this product cannot afford.
func TestSourceDirectedRemediationsReachTheRepairLane(t *testing.T) {
	// One representative log line per classified kind.
	samples := map[projectRuntimeIssueKind]string{
		projectRuntimeIssueMissingModule:  "Error: Cannot find module 'express'",
		projectRuntimeIssueMissingScript:  "npm error Missing script: \"dev\"",
		projectRuntimeIssueSyntax:         "SyntaxError: Unexpected token '}'",
		projectRuntimeIssueCompile:        "Failed to compile.",
		projectRuntimeIssuePortInUse:      "Error: listen EADDRINUSE: address already in use :::3000",
		projectRuntimeIssuePermission:     "Error: EACCES: permission denied, open '/etc/hosts'",
		projectRuntimeIssueCrash:          "process exited: signal: killed",
		projectRuntimeIssueBackingService: "Failed to initialize API Error: The server does not support SSL connections",
	}

	// Phrases that only make sense if the assistant can edit workspace files.
	sourceDirected := []string{
		"in the app's own code",
		"correct the import path",
		"add a",
		"add \"",
		"create ",
		"read the file",
		"apply_patch",
		"package.json",
	}

	for kind, line := range samples {
		issues := projectRuntimeIssuesFromLogs("", []string{line})
		if len(issues) != 1 || issues[0].Kind != kind {
			t.Fatalf("sample for %q classified as %#v", kind, issues)
		}
		issue := issues[0]
		lower := strings.ToLower(issue.Remediation)
		directsAtSource := false
		for _, phrase := range sourceDirected {
			if strings.Contains(lower, phrase) {
				directsAtSource = true
				break
			}
		}
		if directsAtSource && !projectRuntimeIssueSourceFixable(issue) {
			t.Fatalf("%q tells the assistant to change code (%q) but is routed to the read-only operational lane", kind, issue.Remediation)
		}
	}
}

// TestBackingServiceFailureOpensRepairLane is the end-to-end form of the bug a
// user hit: a correct diagnosis that the assistant could not act on.
func TestBackingServiceFailureOpensRepairLane(t *testing.T) {
	issues := projectRuntimeIssuesFromLogs("backend", []string{
		"Failed to initialize API Error: The server does not support SSL connections",
		"process exited: exit status 1",
	})
	result := projectAssistantRuntimeVerificationResult{
		Status: "not_ready",
		Logs:   &projectAssistantRuntimeLogsResult{Blockers: []string{"database"}, Issues: issues},
		Issues: issues,
	}
	if got := projectEinoAssistantRuntimeVerificationDisposition(result); got != projectEinoAssistantVerificationRepair {
		t.Fatalf("disposition = %q, want repair so the assistant can edit the client options it was told to check", got)
	}
}

// TestRecoveredFaultsAreNotReportedAsBlockers is built from two real sandbox
// log windows in which both components were up and serving, while verification
// reported them as failed.
//
// A sandbox log window is a history, not a snapshot: it holds the crashes from
// before the workspace synced alongside the successful start that followed.
// Reporting those told users their working app was broken, which was the
// largest single source of false "Incomplete" runs.
func TestRecoveredFaultsAreNotReportedAsBlockers(t *testing.T) {
	backend := []string{
		"npm error code ENOENT",
		"npm error enoent Could not read package.json: Error: ENOENT: no such file or directory, open '/workspace/package.json'",
		"process exited: exit status 254",
		"[kedge reload] npm install --no-audit --no-fund",
		"up to date in 129ms",
		"> builder-match-api@1.0.0 dev",
		"> node server.js",
		"Builder Match API listening on 8080",
	}
	if issues := projectRuntimeIssuesFromLogs("backend", backend); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none: the API is listening after the failure", issues)
	}

	frontend := []string{
		"process exited: signal: terminated",
		"> builder-match-web@1.0.0 dev",
		"> vite --host 0.0.0.0 --port 8080",
		"  VITE v5.4.21  ready in 191 ms",
		"  ➜  Local:   http://localhost:8080/",
	}
	if issues := projectRuntimeIssuesFromLogs("frontend", frontend); len(issues) != 0 {
		t.Fatalf("issues = %#v, want none: Vite is ready after the termination", issues)
	}
}

// TestFaultsAfterTheLastStartAreStillReported is the other half of the same
// rule: recovery must not mask a process that started and then broke.
func TestFaultsAfterTheLastStartAreStillReported(t *testing.T) {
	lines := []string{
		"Builder Match API listening on 8080",
		"Failed to initialize API Error: The server does not support SSL connections",
		"process exited: exit status 1",
	}
	issues := projectRuntimeIssuesFromLogs("backend", lines)
	if len(issues) == 0 {
		t.Fatal("no issues reported for a process that failed after starting")
	}
	if issues[0].Kind != projectRuntimeIssueBackingService {
		t.Fatalf("first issue = %q, want the failure that followed the successful start", issues[0].Kind)
	}

	// A restart loop — start, fail, start, fail — must still report the fault.
	looping := []string{
		"listening on 8080",
		"Error: Cannot find module 'express'",
		"listening on 8080",
		"Error: Cannot find module 'express'",
	}
	if got := projectRuntimeIssuesFromLogs("backend", looping); len(got) != 1 {
		t.Fatalf("issues = %#v, want the fault still reported in a restart loop", got)
	}
}
