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

// Structured development-runtime issues.
//
// Runtime failures used to reach the assistant as a raw log line: one
// substring match, forwarded verbatim, with the model left to work out what
// broke and what to do about it. That is the expensive rung of the ladder —
// every diagnosis costs a model call, and a misdiagnosis costs a repair turn.
//
// Most dev-server failures are mechanically identifiable and have exactly one
// sensible remediation ("that module is not installed — add it to
// package.json"). Classifying them here means the model receives the diagnosis
// and the fix rather than deriving them, and the same structured issues can be
// fed into the next planning step instead of free text.
//
// Scope: this reads the dev process's own output, so it covers build failures,
// compile errors, unhandled server exceptions, and startup faults. It does NOT
// see browser-side JavaScript errors — nothing in the preview HTTP path is
// owned by kedge, so there is no injection point for a client error shim. See
// docs/app-studio-provider-improvement-plan.md §5.1.

package api

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// projectRuntimeIssueKind identifies a mechanically recognizable failure.
type projectRuntimeIssueKind string

const (
	projectRuntimeIssueMissingModule projectRuntimeIssueKind = "missing_module"
	projectRuntimeIssueMissingScript projectRuntimeIssueKind = "missing_script"
	projectRuntimeIssueSyntax        projectRuntimeIssueKind = "syntax_error"
	projectRuntimeIssueCompile       projectRuntimeIssueKind = "compile_error"
	projectRuntimeIssuePortInUse     projectRuntimeIssueKind = "port_in_use"
	projectRuntimeIssuePermission    projectRuntimeIssueKind = "permission_denied"
	projectRuntimeIssueCrash         projectRuntimeIssueKind = "process_crash"
	// projectRuntimeIssueBackingService covers failures reaching a service the
	// template provisioned (database, cache, queue). Templates ship these, so
	// the app's connection config is a top source of "it does not start" — and
	// without a matcher these fell through to the generic crash entry, whose
	// remediation ("read the lines above the exit") is exactly what the model
	// had already failed to do.
	projectRuntimeIssueBackingService projectRuntimeIssueKind = "backing_service"
)

const (
	projectRuntimeIssueSeverityBlocker = "blocker"
)

// projectRuntimeIssueMaxCount bounds how many distinct issues are reported.
// A failing dev server emits the same fault repeatedly; the first few distinct
// ones carry all the signal, and an unbounded list would crowd the model's
// context with duplicates of one root cause.
const projectRuntimeIssueMaxCount = 8

// projectRuntimeIssueMessageLimit bounds one issue's message.
const projectRuntimeIssueMessageLimit = 240

// projectRuntimeIssue is one diagnosed development-runtime failure.
type projectRuntimeIssue struct {
	Kind      projectRuntimeIssueKind `json:"kind"`
	Severity  string                  `json:"severity"`
	Message   string                  `json:"message"`
	Component string                  `json:"component,omitempty"`
	// File / Line locate the fault in the workspace when the runtime named it.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Subject is the specific thing the issue is about — the missing module or
	// script name, or the port already bound.
	Subject string `json:"subject,omitempty"`
	// Occurrences counts repeats of this same issue in the inspected window.
	Occurrences int `json:"occurrences,omitempty"`
	// Remediation is the deterministic next action for the ASSISTANT. It exists
	// so the cheap rung of the fix ladder can act without spending a model call
	// deriving it. It is written as an instruction to the model — "read the
	// file", "restart the runtime" — and must never be shown to the user, who
	// has no sandbox shell and no restart control.
	Remediation string `json:"remediation,omitempty"`
}

// userExplanation describes the fault to the person watching, in terms of what
// is wrong and who fixes it.
//
// The user-facing blocker list used to render Remediation verbatim, so a build
// failure told a non-technical user to "read the lines above the exit for the
// cause, fix it, then restart the runtime" — three things they cannot do. The
// user needs to know what broke and that the assistant owns the fix.
func (issue projectRuntimeIssue) userExplanation() string {
	switch issue.Kind {
	case projectRuntimeIssueMissingModule:
		if issue.Subject != "" {
			return fmt.Sprintf("The app imports %q, which is not installed or does not exist yet.", issue.Subject)
		}
		return "The app imports something that is not installed or does not exist yet."
	case projectRuntimeIssueMissingScript:
		return "The app is missing the start script its template runs."
	case projectRuntimeIssueSyntax:
		if issue.File != "" {
			return fmt.Sprintf("There is a syntax error in %s.", issue.File)
		}
		return "There is a syntax error in the app's code."
	case projectRuntimeIssueCompile:
		if issue.File != "" {
			return fmt.Sprintf("The app failed to build because of an error in %s.", issue.File)
		}
		return "The app failed to build."
	case projectRuntimeIssuePortInUse:
		return "The app tried to start while a previous copy was still using its port."
	case projectRuntimeIssuePermission:
		return "The app was refused a file or network operation by the sandbox."
	case projectRuntimeIssueBackingService:
		return "The app cannot connect to its database. Its connection settings are wrong — the address itself is provided automatically and is correct."
	case projectRuntimeIssueCrash:
		return "The app started and then exited."
	default:
		return "The app failed to start."
	}
}

// projectRuntimeIssueMatcher recognizes one failure class in a log line.
type projectRuntimeIssueMatcher struct {
	kind     projectRuntimeIssueKind
	severity string
	// pattern must match for the line to be classified.
	pattern *regexp.Regexp
	// subjectGroup names the capture group holding the issue's subject.
	subjectGroup string
	remediation  func(subject string) string
}

var projectRuntimeIssueMatchers = []projectRuntimeIssueMatcher{
	{
		kind:         projectRuntimeIssueMissingModule,
		severity:     projectRuntimeIssueSeverityBlocker,
		pattern:      regexp.MustCompile(`(?i)(?:cannot find module|module not found:?(?: error:)?(?: can't resolve)?|failed to resolve import)\s+['"]?(?P<subject>[^'"\s)]+)`),
		subjectGroup: "subject",
		remediation: func(subject string) string {
			if subject == "" {
				return "Install the missing dependency and re-sync the workspace."
			}
			if strings.HasPrefix(subject, ".") || strings.HasPrefix(subject, "/") {
				return fmt.Sprintf("Create %q in the workspace, or correct the import path that references it.", subject)
			}
			return fmt.Sprintf("Add %q to the component's package.json dependencies, then re-sync so the sandbox installs it.", subject)
		},
	},
	{
		kind:         projectRuntimeIssueMissingScript,
		severity:     projectRuntimeIssueSeverityBlocker,
		pattern:      regexp.MustCompile(`(?i)(?:npm error )?missing script:\s*['"]?(?P<subject>[^'"\s]+)`),
		subjectGroup: "subject",
		remediation: func(subject string) string {
			if subject == "" {
				return "Add the start script the template expects to package.json."
			}
			return fmt.Sprintf("Add a %q script to the component's package.json; the template's start command runs it.", subject)
		},
	},
	{
		kind:     projectRuntimeIssueSyntax,
		severity: projectRuntimeIssueSeverityBlocker,
		pattern:  regexp.MustCompile(`(?i)\bsyntaxerror\b|\bunexpected token\b`),
		remediation: func(string) string {
			return "Read the named file around the reported line and correct the syntax with apply_patch."
		},
	},
	{
		kind:     projectRuntimeIssueCompile,
		severity: projectRuntimeIssueSeverityBlocker,
		pattern:  regexp.MustCompile(`(?i)failed to compile|build failed|compilation error|\bts\d{4}\b:`),
		remediation: func(string) string {
			return "Read the file named in the error and fix the reported type or build failure before re-verifying."
		},
	},
	{
		kind:         projectRuntimeIssuePortInUse,
		severity:     projectRuntimeIssueSeverityBlocker,
		pattern:      regexp.MustCompile(`(?i)(?:eaddrinuse|address already in use)[^0-9]*(?P<subject>\d{2,5})?`),
		subjectGroup: "subject",
		remediation: func(string) string {
			return "The dev process is already bound to its port. Restart the runtime rather than starting a second server; do not change the port, since the template's route targets it."
		},
	},
	{
		kind:     projectRuntimeIssuePermission,
		severity: projectRuntimeIssueSeverityBlocker,
		pattern:  regexp.MustCompile(`(?i)\beacces\b|permission denied`),
		remediation: func(string) string {
			return "The sandbox refused a filesystem or network operation. Keep writes inside the component directory."
		},
	},
	{
		kind:     projectRuntimeIssueBackingService,
		severity: projectRuntimeIssueSeverityBlocker,
		pattern: regexp.MustCompile(`(?i)does not support ssl|ssl (?:is )?(?:not|un)supported|econnrefused|connection refused|` +
			`getaddrinfo (?:enotfound|eai_again)|password authentication failed|no pg_hba\.conf entry|` +
			`connection terminated|timeout expired|could not connect to`),
		remediation: func(string) string {
			// Naming the usual culprit matters: the template injects a working
			// DATABASE_URL, so the app's own connection options are almost
			// always what is wrong — and an SSL-on-by-default heuristic against
			// an in-cluster database is the single most common instance.
			return "The app cannot reach a backing service the template provides. The connection string is injected and correct, so check the client options in the app's own code — " +
				"in particular any TLS/SSL setting inferred from the host name: the development database is in-cluster and does not serve TLS. Do not change the connection string."
		},
	},
	{
		kind:     projectRuntimeIssueCrash,
		severity: projectRuntimeIssueSeverityBlocker,
		pattern:  regexp.MustCompile(`(?i)process exited|unhandled (?:exception|rejection)|fatal error`),
		remediation: func(string) string {
			return "The dev process died. Read the lines above the exit for the cause, fix it, then restart the runtime."
		},
	},
}

// projectRuntimeReadyMarkers recognize a dev server that successfully started.
//
// A sandbox's log window is a history, not a snapshot: it holds the crashes
// from before the workspace was synced alongside the successful start that
// followed. Without a recovery signal every one of those historical failures
// was reported as a current blocker, so a project whose app was up and serving
// was told its runtime had failed — the single largest source of false
// "Incomplete" verifications.
var projectRuntimeReadyMarkers = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`listening on`,
	`server (?:is )?(?:running|started|listening)`,
	`started server`,
	`ready in \d`,
	`ready on`,
	`compiled successfully`,
	`✓ built in`,
	`local:\s+https?://`,
	`accepting connections`,
	`now listening`,
}, "|"))

// projectRuntimeIssueLocation extracts "path:line" style locations that most
// toolchains print, e.g. "/app/src/App.tsx:12:5" or "at src/app.js:9".
var projectRuntimeIssueLocation = regexp.MustCompile(`(?P<file>[\w./@-]+\.[a-zA-Z]{1,5}):(?P<line>\d+)(?::\d+)?`)

// projectRuntimeIssuesFromLogs classifies dev-runtime log lines into distinct,
// deduplicated issues, most severe signal first.
func projectRuntimeIssuesFromLogs(component string, lines []string) []projectRuntimeIssue {
	type aggregate struct {
		issue projectRuntimeIssue
		// last is the index of this fault's most recent appearance. A dev
		// server restarts repeatedly, so the log window holds faults that are
		// already fixed alongside the one blocking it now; ranking by first
		// appearance surfaced the stale one and sent repair after a phantom
		// (observed: a "cannot find module" from before the file was synced
		// outranking the live connection failure).
		last int
	}
	seen := map[string]*aggregate{}
	order := 0

	// Index of the last line showing the process up and serving. Anything that
	// failed before it has already been recovered from.
	lastReady := -1
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if projectRuntimeReadyMarkers.MatchString(line) {
			lastReady = order
			order++
			continue
		}
		matcher, subject, ok := matchProjectRuntimeIssue(line)
		if !ok {
			order++
			continue
		}
		issue := projectRuntimeIssue{
			Kind:        matcher.kind,
			Severity:    matcher.severity,
			Message:     trimProjectAssistantWorkflowString(line, projectRuntimeIssueMessageLimit),
			Component:   component,
			Subject:     subject,
			Occurrences: 1,
		}
		if matcher.remediation != nil {
			issue.Remediation = matcher.remediation(subject)
		}
		issue.File, issue.Line = projectRuntimeIssueFileLine(line)

		// Dedupe on what the issue IS, not on the exact line: a dev server
		// reprints the same fault on every reload with different timestamps.
		key := string(issue.Kind) + "\x00" + issue.Subject + "\x00" + issue.File
		if existing, ok := seen[key]; ok {
			existing.issue.Occurrences++
			existing.last = order
			// Keep the most located instance of the fault.
			if existing.issue.File == "" && issue.File != "" {
				existing.issue.File, existing.issue.Line = issue.File, issue.Line
			}
			order++
			continue
		}
		seen[key] = &aggregate{issue: issue, last: order}
		order++
	}

	out := make([]projectRuntimeIssue, 0, len(seen))
	for _, entry := range seen {
		// The process started successfully after this fault, so it is history.
		// Reporting it would tell the user their working app is broken.
		if lastReady >= 0 && entry.last < lastReady {
			continue
		}
		out = append(out, entry.issue)
	}
	// Blockers first, then most recent — the fault the process is failing on
	// now, not one it already recovered from earlier in the window.
	lastSeen := func(issue projectRuntimeIssue) int {
		if entry, ok := seen[string(issue.Kind)+"\x00"+issue.Subject+"\x00"+issue.File]; ok {
			return entry.last
		}
		return 0
	}
	// A bare "process exited" is a symptom: it says the process died, not why.
	// It is always printed after the fault that killed it, so ranking purely by
	// recency put the symptom above the cause and led with the one entry that
	// has no actionable remediation.
	symptom := func(issue projectRuntimeIssue) bool {
		return issue.Kind == projectRuntimeIssueCrash
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Severity == projectRuntimeIssueSeverityBlocker) != (out[j].Severity == projectRuntimeIssueSeverityBlocker) {
			return out[i].Severity == projectRuntimeIssueSeverityBlocker
		}
		if symptom(out[i]) != symptom(out[j]) {
			return !symptom(out[i])
		}
		return lastSeen(out[i]) > lastSeen(out[j])
	})
	if len(out) > projectRuntimeIssueMaxCount {
		out = out[:projectRuntimeIssueMaxCount]
	}
	return out
}

func matchProjectRuntimeIssue(line string) (projectRuntimeIssueMatcher, string, bool) {
	for _, matcher := range projectRuntimeIssueMatchers {
		match := matcher.pattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		subject := ""
		if matcher.subjectGroup != "" {
			for i, name := range matcher.pattern.SubexpNames() {
				if name == matcher.subjectGroup && i < len(match) {
					subject = strings.TrimSpace(match[i])
				}
			}
		}
		return matcher, subject, true
	}
	return projectRuntimeIssueMatcher{}, "", false
}

// projectRuntimeIssueFileLine pulls a workspace-relative location out of a log
// line. Absolute sandbox paths are trimmed to the workspace-relative form the
// assistant's file tools accept.
func projectRuntimeIssueFileLine(line string) (string, int) {
	match := projectRuntimeIssueLocation.FindStringSubmatch(line)
	if match == nil {
		return "", 0
	}
	file, lineNo := "", 0
	for i, name := range projectRuntimeIssueLocation.SubexpNames() {
		if i >= len(match) {
			break
		}
		switch name {
		case "file":
			file = match[i]
		case "line":
			lineNo, _ = strconv.Atoi(match[i])
		}
	}
	file = strings.TrimPrefix(file, "/app/")
	file = strings.TrimPrefix(file, "./")
	// node_modules frames are the runtime's own code, not the user's.
	if strings.Contains(file, "node_modules/") {
		return "", 0
	}
	return file, lineNo
}

// projectRuntimeIssueSummaries renders issues for the person watching: what
// broke, and who is fixing it. The assistant's own instructions live on
// Issues[].Remediation and are deliberately not repeated here — telling a user
// to restart a runtime they cannot reach turns a status report into a demand.
func projectRuntimeIssueSummaries(issues []projectRuntimeIssue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		summary := issue.userExplanation()
		if issue.Component != "" {
			summary = "[" + issue.Component + "] " + summary
		}
		out = append(out, summary)
	}
	return out
}

// projectRuntimeIssueDiagnosisSummary states what is actually broken, instead
// of the generic "the logs contain a failure" that left the model to go read
// them again.
func projectRuntimeIssueDiagnosisSummary(issues []projectRuntimeIssue) string {
	blockers := make([]projectRuntimeIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.Severity == projectRuntimeIssueSeverityBlocker {
			blockers = append(blockers, issue)
		}
	}
	if len(blockers) == 0 {
		return "The development runtime reported no blocking failures."
	}
	first := blockers[0]
	head := projectRuntimeIssueHeadline(first)
	if len(blockers) > 1 {
		head += fmt.Sprintf(" (and %d other blocking issue(s))", len(blockers)-1)
	}
	// The assistant's instructions are NOT appended here. This summary is
	// rendered to the user, and appending "read the lines above the exit, fix
	// it, then restart the runtime" asked them to do three things they have no
	// way to do. The model reads its remediation from Issues[].Remediation.
	return head
}

// projectRuntimeIssueHeadline describes one issue in a single sentence.
func projectRuntimeIssueHeadline(issue projectRuntimeIssue) string {
	where := ""
	if issue.Component != "" {
		where = " in component " + issue.Component
	}
	if issue.File != "" {
		where += " at " + issue.File
		if issue.Line > 0 {
			where += ":" + strconv.Itoa(issue.Line)
		}
	}
	switch issue.Kind {
	case projectRuntimeIssueMissingModule:
		if issue.Subject != "" {
			return fmt.Sprintf("The development runtime%s cannot resolve %q.", where, issue.Subject)
		}
		return fmt.Sprintf("The development runtime%s cannot resolve an import.", where)
	case projectRuntimeIssueMissingScript:
		if issue.Subject != "" {
			return fmt.Sprintf("The development runtime%s has no %q script to run.", where, issue.Subject)
		}
		return fmt.Sprintf("The development runtime%s is missing its start script.", where)
	case projectRuntimeIssueSyntax:
		return fmt.Sprintf("The development runtime%s failed on a syntax error.", where)
	case projectRuntimeIssueCompile:
		return fmt.Sprintf("The development runtime%s failed to compile.", where)
	case projectRuntimeIssuePortInUse:
		return fmt.Sprintf("The development runtime%s could not bind its port; it is already in use.", where)
	case projectRuntimeIssuePermission:
		return fmt.Sprintf("The development runtime%s was denied a filesystem or network operation.", where)
	case projectRuntimeIssueCrash:
		return fmt.Sprintf("The development process%s exited.", where)
	default:
		return fmt.Sprintf("The development runtime%s reported a failure.", where)
	}
}

// projectRuntimeIssueSourceFixable reports whether editing workspace source is
// the right response to this issue.
//
// This is the cheap rung of the fix ladder. Treating every runtime failure as
// a source defect sent the model editing code for faults that no edit can fix
// — a port already bound, or a denied filesystem operation — burning a repair
// turn and often "fixing" something that was not broken.
func projectRuntimeIssueSourceFixable(issue projectRuntimeIssue) bool {
	switch issue.Kind {
	// A backing service the template provisions is reachable by construction —
	// its address and credentials are injected — so a connection failure is the
	// app's own client configuration, which is source. Classifying it
	// operational sent the assistant to the read-only lane, where it could
	// describe the fault it had correctly diagnosed but had no tool able to fix
	// it, and the run ended by asking the user to resolve it.
	case projectRuntimeIssueMissingModule,
		projectRuntimeIssueMissingScript,
		projectRuntimeIssueSyntax,
		projectRuntimeIssueCompile,
		projectRuntimeIssueBackingService:
		return true
	case projectRuntimeIssuePortInUse, projectRuntimeIssuePermission:
		return false
	case projectRuntimeIssueCrash:
		// A bare exit says the process died, not why. On its own it is an
		// operational signal; when a source fault was diagnosed alongside it,
		// that fault is the cause and is handled by its own entry.
		return false
	default:
		return false
	}
}

// projectRuntimeIssuesSourceFixable reports whether any diagnosed issue calls
// for a source edit.
func projectRuntimeIssuesSourceFixable(issues []projectRuntimeIssue) bool {
	for _, issue := range issues {
		if projectRuntimeIssueSourceFixable(issue) {
			return true
		}
	}
	return false
}

// projectRuntimeIssuesBlocking reports whether any issue prevents the app from
// serving traffic.
func projectRuntimeIssuesBlocking(issues []projectRuntimeIssue) bool {
	for _, issue := range issues {
		if issue.Severity == projectRuntimeIssueSeverityBlocker {
			return true
		}
	}
	return false
}
