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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// projectAssistantToolDisclosureMinimal keeps the action feed opaque: product
// categories and lifecycle state remain visible, while targets and outcomes
// are withheld. The default emits only server-owned, product-facing fields.
var projectAssistantToolDisclosureMinimal = strings.EqualFold(
	strings.TrimSpace(os.Getenv("APP_STUDIO_TOOL_DISCLOSURE")), "minimal")

const (
	projectAssistantActionFeedItemInspect = "inspect"
	projectAssistantActionFeedItemClarify = "clarify"
	projectAssistantActionFeedItemEdit    = "edit"
	projectAssistantActionFeedItemRun     = "run"
	projectAssistantActionFeedItemCommit  = "commit"
	projectAssistantActionFeedItemPlan    = "plan"
	projectAssistantActionFeedItemOther   = "other"
)

const (
	projectAssistantActionFeedStatusRunning   = "running"
	projectAssistantActionFeedStatusWaiting   = "waiting"
	projectAssistantActionFeedStatusSucceeded = "succeeded"
	projectAssistantActionFeedStatusSkipped   = "skipped"
	projectAssistantActionFeedStatusFailed    = "failed"
	projectAssistantActionFeedStatusRejected  = "rejected"
)

const (
	projectAssistantActionFeedSeverityNormal    = "normal"
	projectAssistantActionFeedSeverityAttention = "attention"
	projectAssistantActionFeedSeverityError     = "error"
)

type projectAssistantActionFeedItem struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	Target     string `json:"target,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Count      int    `json:"count,omitempty"`
	Severity   string `json:"severity"`
	GroupKey   string `json:"groupKey,omitempty"`
	GroupTitle string `json:"groupTitle,omitempty"`
	Sequence   int    `json:"sequence,omitempty"`
	// Tool is the base tool name behind the item, carried in memory only (the
	// feed JSON is deliberately opaque — no execution mechanics) so a later
	// success of the same tool+target can absorb an earlier failed attempt:
	// an LLM's self-corrected retry is not a user-facing failure. Absorption
	// degrades gracefully across resumes, where rehydrated items carry no
	// tool name.
	Tool       string                            `json:"-"`
	Diagnostic *projectAssistantActionDiagnostic `json:"diagnostic,omitempty"`
}

type projectAssistantActionDiagnostic struct {
	Category    string `json:"category"`
	Message     string `json:"message"`
	ReferenceID string `json:"referenceID"`
}

func projectAssistantActionFeedItemFromToolCall(toolCall projectToolCallStreamEvent) projectAssistantActionFeedItem {
	item := presentProjectAssistantAction(
		toolCall.ID,
		toolCall.Name,
		toolCall.Status,
		toolCall.Arguments,
		toolCall.Summary,
		toolCall.Error,
	)
	item.Sequence = toolCall.Sequence
	return item
}

func projectAssistantActionFeedItemFromPermission(permission projectAssistantPermission) projectAssistantActionFeedItem {
	return presentProjectAssistantAction(
		permission.ToolCallID,
		permission.ToolName,
		"permission_required",
		"",
		permission.Reason,
		"",
	)
}

func projectAssistantActionFeedItemFromFollowUp(followUp projectAssistantFollowUp) projectAssistantActionFeedItem {
	return presentProjectAssistantAction(
		followUp.ToolCallID,
		projectToolAskFollowUp,
		"input_required",
		"",
		followUp.Prompt,
		"",
	)
}

func presentProjectAssistantAction(id, name, rawStatus, arguments, summary, errText string) projectAssistantActionFeedItem {
	kind := projectAssistantActionFeedItemKind(name)
	status := projectAssistantActionFeedItemStatus(rawStatus)
	item := projectAssistantActionFeedItem{
		ID:       projectAssistantActionPublicID(id),
		Kind:     kind,
		Status:   status,
		Title:    projectAssistantActionFeedItemTitle(kind, status),
		Severity: projectAssistantActionFeedItemSeverity(status),
	}
	if status == projectAssistantActionFeedStatusFailed || status == projectAssistantActionFeedStatusRejected {
		item.Diagnostic = projectAssistantActionFeedDiagnostic(id, errText)
	}
	if projectAssistantToolDisclosureMinimal {
		return item
	}

	args := projectAssistantActionArguments(arguments)
	base := projectToolBaseName(name)
	item.Tool = base
	switch base {
	case projectToolReadFile:
		path := projectAssistantActionArgumentField(args, arguments, "file_path", "path")
		if projectEinoAssistantFilesystemReadTool(name) {
			if decoded, ok := unescapeProjectCanonicalToolSummaryValue(path); ok {
				path = decoded
			} else {
				path = ""
			}
		}
		item.Target = projectAssistantActionSafeTarget(path)
		item.Title = projectAssistantActionLifecycleTitle(status, "Reading file", "Read file", "File read failed")
	case projectToolLS, projectToolGlob:
		item.Title = projectAssistantActionLifecycleTitle(status, "Reading project files", "Read project files", "Project inspection failed")
		if count, ok := projectAssistantSummaryCount(summary, "path(s)"); ok {
			item.Count = count
			item.Outcome = projectAssistantCountOutcome(count, "project file", "project files")
		}
	case projectToolGrep:
		item.Title = projectAssistantActionLifecycleTitle(status, "Searching project", "Searched project", "Project search failed")
		if count, ok := projectAssistantSummaryCount(summary, "result line(s)"); ok {
			item.Count = count
			item.Outcome = projectAssistantCountOutcome(count, "reference", "references")
		}
	case projectToolWriteFile, projectToolApplyPatch:
		item.Target = projectAssistantActionSafeTarget(projectAssistantActionArgumentField(args, arguments, "path", "path"))
		item.Title = projectAssistantActionLifecycleTitle(status, "Updating file", "Updated file", "File update failed")
		if status == projectAssistantActionFeedStatusSucceeded {
			item.Outcome = projectAssistantMutationOutcome(summary)
		}
	case projectToolMkdir:
		item.Target = projectAssistantActionSafeTarget(projectAssistantActionArgumentField(args, arguments, "path", "path"))
		item.Title = projectAssistantActionLifecycleTitle(status, "Creating folder", "Created folder", "Folder creation failed")
	case projectToolVerifyDevelopmentRuntime:
		item.Title = projectAssistantActionLifecycleTitle(status, "Checking development preview", "Checked development preview", "Preview check failed")
	case projectToolGetPreviewURL:
		item.Title = projectAssistantActionLifecycleTitle(status, "Checking preview", "Checked preview", "Preview check failed")
	case projectToolCheckProjectReadiness:
		item.Title = projectAssistantActionLifecycleTitle(status, "Checking project readiness", "Checked project readiness", "Readiness check failed")
	case projectToolPrepareProjectDeployment:
		item.Title = projectAssistantActionLifecycleTitle(status, "Preparing development preview", "Prepared development preview", "Preview preparation failed")
	case projectToolGetRuntimeStatus:
		item.Title = projectAssistantActionLifecycleTitle(status, "Checking development runtime", "Checked development runtime", "Runtime check failed")
	case projectToolGetRuntimeLogs:
		item.Title = projectAssistantActionLifecycleTitle(status, "Reviewing runtime logs", "Reviewed runtime logs", "Runtime log review failed")
		if count, ok := projectAssistantSummaryCount(summary, "log line(s)"); ok {
			item.Count = count
			item.Outcome = projectAssistantCountOutcome(count, "log line", "log lines")
		}
	case projectToolGetPreviewConsoleLogs:
		item.Title = projectAssistantActionLifecycleTitle(status, "Reviewing preview console", "Reviewed preview console", "Preview console review failed")
		if count, ok := projectAssistantSummaryCount(summary, "browser console event(s)"); ok {
			item.Count = count
			item.Outcome = projectAssistantCountOutcome(count, "console event", "console events")
		}
	case projectToolRestartRuntime:
		item.Title = projectAssistantActionLifecycleTitle(status, "Restarting development runtime", "Restarted development runtime", "Runtime restart failed")
	case projectToolSetRuntimeEnv:
		item.Title = projectAssistantActionLifecycleTitle(status, "Updating environment", "Updated environment", "Environment update failed")
		if env, ok := args["env"].(map[string]any); ok {
			item.Count = len(env)
			item.Outcome = projectAssistantCountOutcome(len(env), "environment variable", "environment variables")
		} else if count, ok := projectAssistantSummaryCount(arguments, "variable(s):"); ok {
			item.Count = count
			item.Outcome = projectAssistantCountOutcome(count, "environment variable", "environment variables")
		}
	case projectToolCommitFiles, projectToolCommitProjectFiles:
		item.Title = projectAssistantActionLifecycleTitle(status, "Committing changes", "Committed changes", "Commit failed")
		paths := projectToolFilePaths(args["files"])
		if len(paths) == 0 {
			paths = projectToolStringList(args["paths"])
		}
		if len(paths) > 0 {
			item.Count = len(paths)
			item.Outcome = projectAssistantCountOutcome(len(paths), "file", "files")
		} else if count, ok := projectAssistantSummaryCount(arguments, "file(s):"); ok {
			item.Count = count
			item.Outcome = projectAssistantCountOutcome(count, "file", "files")
		}
		if ref := projectAssistantSummaryField(summary, "commit"); ref != "" {
			item.Target = projectAssistantActionSafeTarget(ref)
		}
	}
	projectAssistantActionFeedGrouping(&item, base)
	return item
}

func projectAssistantMutationOutcome(summary string) string {
	parts := strings.Split(summary, ";")
	counts := make([]string, 0, 2)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "+") || strings.HasPrefix(part, "-") {
			counts = append(counts, part)
		}
	}
	return strings.Join(counts, " ")
}

func projectAssistantActionPublicID(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return "feed-" + hex.EncodeToString(sum[:12])
}

func projectAssistantActionFeedItemKind(name string) string {
	switch base := projectToolBaseName(name); {
	case base == projectToolAskFollowUp:
		return projectAssistantActionFeedItemClarify
	case base == projectToolRequestProjectPlanApproval || base == projectToolPlanProjectChanges:
		return projectAssistantActionFeedItemPlan
	case base == projectToolCheckProjectReadiness || base == projectToolPrepareProjectDeployment ||
		base == projectToolVerifyDevelopmentRuntime || base == projectToolGetRuntimeStatus ||
		base == projectToolGetPreviewURL || base == projectToolGetRuntimeLogs ||
		base == projectToolRestartRuntime || base == projectToolSetRuntimeEnv:
		return projectAssistantActionFeedItemRun
	case base == projectToolCommitProjectFiles || base == projectToolCommitFiles:
		return projectAssistantActionFeedItemCommit
	case base == projectToolWriteFile || base == projectToolApplyPatch || base == projectToolMkdir:
		return projectAssistantActionFeedItemEdit
	case base == projectToolLS || base == projectToolReadFile || base == projectToolGlob || base == projectToolGrep:
		return projectAssistantActionFeedItemInspect
	default:
		return projectAssistantActionFeedItemOther
	}
}

func projectAssistantActionFeedItemStatus(status string) string {
	switch status {
	case "", "requested", "running":
		return projectAssistantActionFeedStatusRunning
	case "permission_required", "input_required":
		return projectAssistantActionFeedStatusWaiting
	case "failed":
		return projectAssistantActionFeedStatusFailed
	case "rejected":
		return projectAssistantActionFeedStatusRejected
	case "skipped":
		return projectAssistantActionFeedStatusSkipped
	default:
		return projectAssistantActionFeedStatusSucceeded
	}
}

func projectAssistantActionFeedItemSeverity(status string) string {
	switch status {
	case projectAssistantActionFeedStatusWaiting:
		return projectAssistantActionFeedSeverityAttention
	case projectAssistantActionFeedStatusFailed, projectAssistantActionFeedStatusRejected:
		return projectAssistantActionFeedSeverityError
	default:
		return projectAssistantActionFeedSeverityNormal
	}
}

func projectAssistantActionFeedItemTitle(kind, status string) string {
	switch kind {
	case projectAssistantActionFeedItemInspect:
		return projectAssistantActionLifecycleTitle(status, "Inspecting project", "Inspected project", "Inspection failed")
	case projectAssistantActionFeedItemEdit:
		return projectAssistantActionLifecycleTitle(status, "Editing files", "Edited files", "Edit failed")
	case projectAssistantActionFeedItemRun:
		return projectAssistantActionLifecycleTitle(status, "Running checks", "Ran checks", "Run failed")
	case projectAssistantActionFeedItemCommit:
		return projectAssistantActionLifecycleTitle(status, "Preparing commit", "Committed changes", "Commit failed")
	case projectAssistantActionFeedItemPlan:
		return projectAssistantActionLifecycleTitle(status, "Reviewing plan", "Reviewed plan", "Plan rejected")
	case projectAssistantActionFeedItemClarify:
		return projectAssistantActionLifecycleTitle(status, "Clarifying requirements", "Clarified requirements", "Clarification failed")
	default:
		return projectAssistantActionLifecycleTitle(status, "Working", "Completed action", "Action failed")
	}
}

func projectAssistantActionLifecycleTitle(status, active, succeeded, failed string) string {
	switch status {
	case projectAssistantActionFeedStatusRunning, projectAssistantActionFeedStatusWaiting:
		return active
	case projectAssistantActionFeedStatusFailed, projectAssistantActionFeedStatusRejected:
		return failed
	case projectAssistantActionFeedStatusSkipped:
		return "Skipped duplicate read"
	default:
		return succeeded
	}
}

func projectAssistantActionFeedGrouping(item *projectAssistantActionFeedItem, base string) {
	if item == nil || item.Status != projectAssistantActionFeedStatusSucceeded {
		return
	}
	switch base {
	case projectToolReadFile, projectToolLS, projectToolGlob:
		item.GroupKey = "inspect:files"
		item.GroupTitle = "Read project files"
	case projectToolGrep:
		item.GroupKey = "inspect:search"
		item.GroupTitle = "Searched project"
	case projectToolWriteFile, projectToolApplyPatch, projectToolDeleteFile, projectToolMkdir:
		item.GroupKey = "edit:files"
		item.GroupTitle = "Updated files"
	case projectToolCheckProjectReadiness, projectToolPrepareProjectDeployment,
		projectToolVerifyDevelopmentRuntime, projectToolGetRuntimeStatus,
		projectToolGetPreviewURL, projectToolGetRuntimeLogs, projectToolRestartRuntime:
		item.GroupKey = "run:checks"
		item.GroupTitle = "Ran checks"
	}
}

func projectAssistantActionArguments(raw string) map[string]any {
	var args map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &args) != nil {
		return nil
	}
	return args
}

func projectAssistantActionArgumentField(args map[string]any, summary, rawKey, summaryKey string) string {
	if value := projectToolString(args[rawKey]); value != "" {
		return value
	}
	return projectAssistantSummaryField(summary, summaryKey)
}

func projectAssistantSummaryField(summary, field string) string {
	prefix := field + " "
	for _, part := range strings.Split(summary, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(part, prefix))
		}
	}
	return ""
}

func projectAssistantSummaryCount(summary, marker string) (int, bool) {
	index := strings.Index(summary, marker)
	if index < 0 {
		return 0, false
	}
	fields := strings.Fields(strings.TrimSpace(summary[:index]))
	if len(fields) == 0 {
		return 0, false
	}
	count, err := strconv.Atoi(strings.Trim(fields[len(fields)-1], ";"))
	return count, err == nil && count >= 0
}

func projectAssistantCountOutcome(count int, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func projectAssistantActionSafeTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	const maxRunes = 240
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes-3]) + "..."
	}
	return value
}

func projectAssistantActionFeedDiagnostic(id, rawError string) *projectAssistantActionDiagnostic {
	category := projectAssistantActionDiagnosticCategory(rawError)
	message := map[string]string{
		"timeout":    "The action did not finish before its time limit.",
		"permission": "App Studio did not have permission to complete this action.",
		"validation": "The action could not run because its input was not valid.",
		"runtime":    "The development runtime could not complete this action.",
		"provider":   "A connected provider could not complete this action.",
		"unknown":    "App Studio could not complete this action.",
	}[category]
	sum := sha256.Sum256([]byte(id))
	return &projectAssistantActionDiagnostic{
		Category:    category,
		Message:     message,
		ReferenceID: "action-" + hex.EncodeToString(sum[:6]),
	}
}

func projectAssistantActionDiagnosticCategory(raw string) string {
	value := strings.ToLower(raw)
	switch {
	case strings.Contains(value, "timeout"), strings.Contains(value, "timed out"), strings.Contains(value, "deadline exceeded"):
		return "timeout"
	case strings.Contains(value, "permission"), strings.Contains(value, "forbidden"), strings.Contains(value, "unauthorized"), strings.Contains(value, "access denied"),
		strings.Contains(value, "plan approval required"), strings.Contains(value, "execution plan revision required"):
		return "permission"
	case strings.Contains(value, "validation"), strings.Contains(value, "invalid"), strings.Contains(value, "malformed"),
		strings.Contains(value, "required"), strings.Contains(value, "repository binding"):
		return "validation"
	case strings.Contains(value, "runtime"), strings.Contains(value, "preview"), strings.Contains(value, "process exited"), strings.Contains(value, "server did not"):
		return "runtime"
	case strings.Contains(value, "provider"), strings.Contains(value, "mcp"), strings.Contains(value, "upstream"):
		return "provider"
	default:
		return "unknown"
	}
}

func projectAssistantMessageMetadata(status string, toolCalls []projectToolCallStreamEvent) map[string]any {
	metadata := map[string]any{}
	if status != "" {
		metadata[projectMessageMetadataStatus] = status
	}
	if actions := projectAssistantActionFeedFromToolCalls(toolCalls); len(actions) > 0 {
		metadata[projectMessageMetadataAssistantActionFeed] = actions
	}
	if interrupt := projectAssistantUIInterruptFromToolCalls(toolCalls); interrupt != nil {
		metadata[projectMessageMetadataAssistantInterrupt] = interrupt
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func projectAssistantActionFeedFromToolCalls(events []projectToolCallStreamEvent) []projectAssistantActionFeedItem {
	return filterProjectAssistantActionFeedItems(projectAssistantActionFeedUpdatesFromToolCalls(events))
}

func projectAssistantActionFeedUpdatesFromToolCalls(events []projectToolCallStreamEvent) []projectAssistantActionFeedItem {
	if len(events) == 0 {
		return nil
	}
	actions := make([]projectAssistantActionFeedItem, 0, len(events))
	for _, event := range events {
		if event.ID == "" || event.Status == "" || projectToolBaseName(event.Name) == projectEinoAssistantWriteTodosTool {
			continue
		}
		actions = upsertProjectAssistantActionFeedItem(actions, projectAssistantActionFeedItemFromToolCall(event))
	}
	if len(actions) == 0 {
		return nil
	}
	return actions
}

func projectAssistantUIInterruptFromToolCalls(events []projectToolCallStreamEvent) *projectAssistantUIInterruptRequest {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Status == "input_required" && event.FollowUp != nil && event.Checkpoint != nil {
			return projectAssistantUIInterruptRequestFromFollowUpCheckpoint("", *event.FollowUp, *event.Checkpoint)
		}
		if event.Status != "permission_required" || event.Permission == nil || event.Checkpoint == nil {
			continue
		}
		return projectAssistantUIInterruptRequestFromPermissionCheckpoint("", *event.Permission, *event.Checkpoint)
	}
	return nil
}

func projectAssistantActionFeedFromMetadata(raw any) []projectAssistantActionFeedItem {
	if raw == nil {
		return nil
	}
	if typed, ok := raw.([]projectAssistantActionFeedItem); ok {
		return filterProjectAssistantActionFeedItems(typed)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []projectAssistantActionFeedItem
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return filterProjectAssistantActionFeedItems(out)
}

func filterProjectAssistantActionFeedItems(items []projectAssistantActionFeedItem) []projectAssistantActionFeedItem {
	filtered := make([]projectAssistantActionFeedItem, 0, len(items))
	for _, item := range items {
		if projectAssistantActionFeedItemVisible(item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func projectAssistantActionFeedItemVisible(item projectAssistantActionFeedItem) bool {
	if item.Kind != projectAssistantActionFeedItemOther {
		return true
	}
	return item.Status == projectAssistantActionFeedStatusWaiting ||
		item.Status == projectAssistantActionFeedStatusFailed ||
		item.Status == projectAssistantActionFeedStatusRejected
}

func projectAssistantUIInterruptFromMetadata(raw any) *projectAssistantUIInterruptRequest {
	if raw == nil {
		return nil
	}
	if typed, ok := raw.(*projectAssistantUIInterruptRequest); ok {
		if typed == nil {
			return nil
		}
		copy := *typed
		return &copy
	}
	if typed, ok := raw.(projectAssistantUIInterruptRequest); ok {
		return &typed
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out projectAssistantUIInterruptRequest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	if out.InterruptID == "" && out.Action == nil {
		return nil
	}
	return &out
}

func upsertProjectAssistantActionFeedItem(actions []projectAssistantActionFeedItem, action projectAssistantActionFeedItem) []projectAssistantActionFeedItem {
	if action.ID == "" {
		return actions
	}
	// A success absorbs earlier FAILED attempts of the same tool on the same
	// target: the model retried and recovered, so the failed attempt is
	// self-corrected noise, not a user-facing error. Distinct targets and
	// still-unrecovered failures keep their cards.
	if action.Status == projectAssistantActionFeedStatusSucceeded && action.Tool != "" {
		filtered := actions[:0]
		for _, existing := range actions {
			if existing.ID != action.ID &&
				existing.Status == projectAssistantActionFeedStatusFailed &&
				existing.Tool == action.Tool &&
				existing.Target == action.Target {
				continue
			}
			filtered = append(filtered, existing)
		}
		actions = filtered
	}
	for i := range actions {
		if actions[i].ID == action.ID {
			actions[i] = mergeProjectAssistantActionFeedItem(actions[i], action)
			return actions
		}
	}
	return append(actions, action)
}

func applyProjectAssistantActionFeedUpdate(actions []projectAssistantActionFeedItem, action projectAssistantActionFeedItem) []projectAssistantActionFeedItem {
	if projectAssistantActionFeedItemVisible(action) {
		return upsertProjectAssistantActionFeedItem(actions, action)
	}
	filtered := actions[:0]
	for _, existing := range actions {
		if existing.ID != action.ID {
			filtered = append(filtered, existing)
		}
	}
	return filtered
}

func mergeProjectAssistantActionFeedItem(existing, next projectAssistantActionFeedItem) projectAssistantActionFeedItem {
	if next.Kind == "" {
		next.Kind = existing.Kind
	}
	if next.Status == "" {
		next.Status = existing.Status
	}
	if next.Title == "" {
		next.Title = existing.Title
	}
	if next.Target == "" {
		next.Target = existing.Target
	}
	if next.Tool == "" {
		next.Tool = existing.Tool
	}
	if next.Outcome == "" {
		next.Outcome = existing.Outcome
	}
	if next.Count == 0 {
		next.Count = existing.Count
	}
	if next.Severity == "" {
		next.Severity = existing.Severity
	}
	if next.GroupKey == "" {
		next.GroupKey = existing.GroupKey
	}
	if next.GroupTitle == "" {
		next.GroupTitle = existing.GroupTitle
	}
	if next.Sequence == 0 {
		next.Sequence = existing.Sequence
	}
	if next.Diagnostic == nil {
		next.Diagnostic = existing.Diagnostic
	}
	return next
}

// projectAssistantMetadataMaxToolCalls bounds how many tool-call events are
// carried in durable message metadata.
//
// Metadata is re-serialized and saved on every tool event, and streamed inside
// every SSE snapshot, so its size is paid O(n) times over a run — quadratic in
// bytes. A long mutation-heavy run accumulated multi-megabyte metadata that
// stalled the store and spammed subscribers. The most recent calls are what
// the UI renders; older ones live in the audit record.
const projectAssistantMetadataMaxToolCalls = 64

func sanitizeProjectToolCallStreamEventsForMetadata(events []projectToolCallStreamEvent) []projectToolCallStreamEvent {
	if len(events) == 0 {
		return nil
	}
	// Keep the newest events, and always keep the first: it anchors the run in
	// the UI and is what a reader looks for when scrolling back.
	trimmed := events
	if len(events) > projectAssistantMetadataMaxToolCalls {
		trimmed = make([]projectToolCallStreamEvent, 0, projectAssistantMetadataMaxToolCalls)
		trimmed = append(trimmed, events[0])
		trimmed = append(trimmed, events[len(events)-(projectAssistantMetadataMaxToolCalls-1):]...)
	}
	out := make([]projectToolCallStreamEvent, 0, len(trimmed))
	for _, event := range trimmed {
		if event.Permission != nil {
			permission := *event.Permission
			permission.Input = nil
			event.Permission = &permission
		}
		if event.Mutation != nil {
			// The patch body is the bulk of an edit event and is already
			// durable twice over — in the run's audit record and in the undo
			// snapshot. Metadata only needs the counts it renders.
			mutation := *event.Mutation
			mutation.Patch = ""
			mutation.PatchTruncated = false
			event.Mutation = &mutation
		}
		out = append(out, event)
	}
	return out
}

func projectToolCallStreamEventsFromMetadata(raw any) []projectToolCallStreamEvent {
	if raw == nil {
		return nil
	}
	if typed, ok := raw.([]projectToolCallStreamEvent); ok {
		return append([]projectToolCallStreamEvent(nil), typed...)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []projectToolCallStreamEvent
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func upsertProjectToolCallStreamEvent(events []projectToolCallStreamEvent, event projectToolCallStreamEvent) []projectToolCallStreamEvent {
	for i := range events {
		if events[i].ID == event.ID {
			events[i] = mergeProjectToolCallStreamEvent(events[i], event)
			return events
		}
	}
	return append(events, event)
}

func mergeProjectToolCallStreamEvent(existing, next projectToolCallStreamEvent) projectToolCallStreamEvent {
	if next.Name == "" {
		next.Name = existing.Name
	}
	if next.Arguments == "" {
		next.Arguments = existing.Arguments
	}
	if next.Summary == "" {
		next.Summary = existing.Summary
	}
	if next.Error == "" {
		next.Error = existing.Error
	}
	if next.Permission == nil {
		next.Permission = existing.Permission
	}
	if next.FollowUp == nil {
		next.FollowUp = existing.FollowUp
	}
	if next.Checkpoint == nil {
		next.Checkpoint = existing.Checkpoint
	}
	if next.Sequence == 0 {
		next.Sequence = existing.Sequence
	}
	if next.Mutation == nil {
		next.Mutation = existing.Mutation
	}
	return next
}
