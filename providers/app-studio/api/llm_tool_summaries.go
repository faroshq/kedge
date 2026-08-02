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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Human-readable summaries of assistant tool calls and results for the
// action feed and audit trail, plus the any-coercion helpers they need.

func ensureProjectToolCallIDs(toolCalls []chatToolCall) {
	for i := range toolCalls {
		if toolCalls[i].ID == "" {
			toolCalls[i].ID = fmt.Sprintf("tool-%d", i+1)
		}
	}
}

func summarizeProjectToolArguments(name, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "unparseable arguments"
	}
	return summarizeProjectToolArgumentsMap(name, args)
}

func summarizeProjectToolArgumentsMap(name string, args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	switch projectToolBaseName(name) {
	case projectToolCommitFiles, projectToolCommitProjectFiles:
		parts := []string{}
		if repo := projectToolString(args["repositoryRef"]); repo != "" {
			parts = append(parts, "repository "+repo)
		}
		if branch := projectToolString(args["branch"]); branch != "" {
			parts = append(parts, "branch "+branch)
		}
		if message := projectToolString(args["message"]); message != "" {
			parts = append(parts, "message "+message)
		}
		paths := projectToolFilePaths(args["files"])
		if len(paths) == 0 {
			paths = projectToolStringList(args["paths"])
		}
		if len(paths) > 0 {
			parts = append(parts, fmt.Sprintf("%d file(s): %s", len(paths), summarizeProjectToolList(paths, 5)))
		}
		return truncateProjectToolInfo(strings.Join(parts, "; "))
	case projectToolLS:
		return summarizeProjectCanonicalToolKeyValues(args, []string{"path"})
	case projectToolReadFile:
		return summarizeProjectCanonicalToolKeyValues(map[string]any{
			"path":   args["file_path"],
			"offset": args["offset"],
			"limit":  args["limit"],
		}, []string{"path", "offset", "limit"})
	case projectToolGlob:
		return summarizeProjectCanonicalToolKeyValues(args, []string{"path", "pattern"})
	case projectToolGrep:
		return summarizeProjectCanonicalToolKeyValues(args, []string{
			"path", "pattern", "glob", "type", "output_mode",
			"-C", "-B", "-A", "-n", "-i", "head_limit", "offset", "multiline",
		})
	case projectToolPlanProjectChanges, projectToolCheckProjectReadiness, projectToolPrepareProjectDeployment:
		return summarizeProjectPlanningWorkflowArgs(args)
	case projectToolGetRuntimeStatus, projectToolGetPreviewURL, projectToolGetPreviewConsoleLogs, projectToolRestartRuntime:
		return ""
	case projectToolGetRuntimeLogs:
		return summarizeProjectToolKeyValues(args, []string{"tailLines"})
	case projectToolSetRuntimeEnv:
		if env, ok := args["env"].(map[string]any); ok && len(env) > 0 {
			names := make([]string, 0, len(env))
			for name := range env {
				names = append(names, name)
			}
			sort.Strings(names)
			return truncateProjectToolInfo(fmt.Sprintf("%d variable(s): %s", len(names), summarizeProjectToolList(names, 5)))
		}
		return ""
	case projectToolAskFollowUp:
		if questions := projectToolStringList(args["questions"]); len(questions) > 0 {
			return truncateProjectToolInfo(fmt.Sprintf("%d question(s): %s", len(questions), summarizeProjectToolList(questions, 3)))
		}
		return ""
	case projectToolRequestProjectPlanApproval:
		parts := []string{}
		if summary := projectToolString(args["summary"]); summary != "" {
			parts = append(parts, summary)
		}
		if paths := projectToolStringList(args["targetPaths"]); len(paths) > 0 {
			parts = append(parts, fmt.Sprintf("%d target path(s): %s", len(paths), summarizeProjectToolList(paths, 5)))
		}
		return truncateProjectToolInfo(strings.Join(parts, "; "))
	case projectToolWriteFile:
		return summarizeProjectMutationArgs(args, []string{"path"}, true)
	case projectToolApplyPatch:
		return summarizeProjectMutationArgs(args, []string{"path"}, false)
	case projectToolMkdir:
		return summarizeProjectToolKeyValues(args, []string{"path"})
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return truncateProjectToolInfo(string(raw))
}

func summarizeProjectToolResult(name, result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	switch projectToolBaseName(name) {
	case projectToolReadFile:
		return "file read"
	case projectToolLS, projectToolGlob:
		return fmt.Sprintf("%d path(s)", projectAssistantNonEmptyLineCount(result))
	case projectToolGrep:
		return fmt.Sprintf("%d result line(s)", projectAssistantGrepResultLineCount(result))
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(result), &decoded); err == nil {
		switch projectToolBaseName(name) {
		case projectToolCommitFiles, projectToolCommitProjectFiles:
			parts := []string{}
			if sha := projectToolString(decoded["commitSHA"]); sha != "" {
				if len(sha) > 12 {
					sha = sha[:12]
				}
				parts = append(parts, "commit "+sha)
			} else if reqName := projectToolString(decoded["name"]); reqName != "" {
				parts = append(parts, "request "+reqName)
			}
			if phase := projectToolString(decoded["phase"]); phase != "" {
				parts = append(parts, "phase "+phase)
			}
			if branch := projectToolString(decoded["branch"]); branch != "" {
				parts = append(parts, "branch "+branch)
			}
			if files := projectToolStringList(decoded["files"]); len(files) > 0 {
				parts = append(parts, fmt.Sprintf("%d file(s): %s", len(files), summarizeProjectToolList(files, 5)))
			}
			if len(parts) > 0 {
				return truncateProjectToolInfo(strings.Join(parts, "; "))
			}
		case projectToolPlanProjectChanges:
			return summarizeProjectPlanningWorkflowResult(decoded)
		case projectToolRequestProjectPlanApproval:
			parts := []string{}
			if status := projectToolString(decoded["status"]); status != "" {
				parts = append(parts, "status "+status)
			}
			if summary := projectToolString(decoded["summary"]); summary != "" {
				parts = append(parts, summary)
			}
			if paths := projectToolStringList(decoded["targetPaths"]); len(paths) > 0 {
				parts = append(parts, fmt.Sprintf("%d target path(s): %s", len(paths), summarizeProjectToolList(paths, 5)))
			}
			if len(parts) > 0 {
				return truncateProjectToolInfo(strings.Join(parts, "; "))
			}
		case projectToolCheckProjectReadiness, projectToolPrepareProjectDeployment:
			return summarizeProjectReadinessWorkflowResult(decoded)
		case projectToolGetRuntimeStatus, projectToolGetPreviewURL, projectToolRestartRuntime, projectToolSetRuntimeEnv:
			return summarizeProjectRuntimeWorkflowResult(decoded)
		case projectToolGetRuntimeLogs:
			if lines := projectToolStringList(decoded["lines"]); len(lines) > 0 {
				return truncateProjectToolInfo(fmt.Sprintf("%d log line(s)", len(lines)))
			}
			if summary := projectToolString(decoded["summary"]); summary != "" {
				return truncateProjectToolInfo(summary)
			}
		case projectToolGetPreviewConsoleLogs:
			if summary := projectToolString(decoded["summary"]); summary != "" {
				return truncateProjectToolInfo(summary)
			}
			if status := projectToolString(decoded["status"]); status != "" {
				return "status " + status
			}
		case projectToolAskFollowUp:
			if answer := projectToolString(decoded["answer"]); answer != "" {
				return truncateProjectToolInfo("answered: " + answer)
			}
		case projectToolWriteFile, projectToolApplyPatch, projectToolMkdir:
			return summarizeWorkspaceMutationResult(decoded)
		}
		if message := projectToolString(decoded["message"]); message != "" {
			return truncateProjectToolInfo(message)
		}
	}
	firstLine := strings.TrimSpace(strings.Split(result, "\n")[0])
	return truncateProjectToolInfo(firstLine)
}

func summarizeProjectMutationArgs(args map[string]any, keys []string, includeContentBytes bool) string {
	parts := []string{}
	if summary := summarizeProjectToolKeyValues(args, keys); summary != "" {
		parts = append(parts, summary)
	}
	if includeContentBytes {
		if content, ok := args["content"].(string); ok {
			parts = append(parts, fmt.Sprintf("%d bytes", len([]byte(content))))
		}
	}
	if projectToolBool(args["replaceAll"]) {
		parts = append(parts, "replaceAll")
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeProjectToolKeyValues(args map[string]any, keys []string) string {
	parts := []string{}
	for _, key := range keys {
		switch key {
		case "maxBytes", "maxResults", "limit", "offset", "head_limit", "-C", "-B", "-A":
			if n, ok := projectToolNumber(args[key]); ok {
				parts = append(parts, fmt.Sprintf("%s %d", key, n))
			}
		case "-n", "-i", "multiline":
			if value, ok := args[key].(bool); ok {
				parts = append(parts, fmt.Sprintf("%s %t", key, value))
			}
		default:
			if value := projectToolString(args[key]); value != "" {
				parts = append(parts, key+" "+value)
			}
		}
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeProjectCanonicalToolKeyValues(args map[string]any, keys []string) string {
	safeArgs := make(map[string]any, len(keys))
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			safeArgs[key] = escapeProjectCanonicalToolSummaryValue(text)
			continue
		}
		safeArgs[key] = value
	}
	return summarizeProjectToolKeyValues(safeArgs, keys)
}

func escapeProjectCanonicalToolSummaryValue(value string) string {
	const hex = "0123456789ABCDEF"
	var escaped strings.Builder
	for _, r := range value {
		if r != ';' && r != '%' && !unicode.IsControl(r) {
			escaped.WriteRune(r)
			continue
		}
		var encoded [utf8.UTFMax]byte
		n := utf8.EncodeRune(encoded[:], r)
		for _, b := range encoded[:n] {
			escaped.WriteByte('%')
			escaped.WriteByte(hex[b>>4])
			escaped.WriteByte(hex[b&0x0f])
		}
	}
	return escaped.String()
}

func unescapeProjectCanonicalToolSummaryValue(value string) (string, bool) {
	var unescaped strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			unescaped.WriteByte(value[i])
			continue
		}
		if i+2 >= len(value) {
			return "", false
		}
		high, ok := projectAssistantHexNibble(value[i+1])
		if !ok {
			return "", false
		}
		low, ok := projectAssistantHexNibble(value[i+2])
		if !ok {
			return "", false
		}
		unescaped.WriteByte(high<<4 | low)
		i += 2
	}
	decoded := unescaped.String()
	if !utf8.ValidString(decoded) || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return "", false
	}
	return decoded, true
}

func projectAssistantHexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func projectAssistantCanonicalFilesystemReadTool(name string) bool {
	switch strings.TrimSpace(name) {
	case projectToolLS, projectToolReadFile, projectToolGlob, projectToolGrep:
		return true
	default:
		return false
	}
}

func projectAssistantNonEmptyLineCount(value string) int {
	value = strings.TrimSpace(value)
	if value == "" || value == "No files found" || value == "No matches found" {
		return 0
	}
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func projectAssistantGrepResultLineCount(value string) int {
	value = strings.TrimSpace(value)
	if value == "" || value == "No matches found" || value == "No files found" {
		return 0
	}
	lines := strings.Split(value, "\n")
	first := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			first = i
			break
		}
	}
	if first >= 0 {
		if total, ok := projectAssistantGrepFilesHeader(strings.TrimSpace(lines[first])); ok {
			return total
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if count, ok := projectAssistantGrepCountTrailer(line); ok {
			return count
		}
		break
	}
	return projectAssistantNonEmptyLineCount(value)
}

func summarizeProjectEinoGrepResult(args map[string]any, result string) string {
	mode, _ := args["output_mode"].(string)
	count := 0
	switch mode {
	case "content":
		count = projectAssistantNonEmptyLineCount(result)
	case "count":
		lines := strings.Split(strings.TrimSpace(result), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			count, _ = projectAssistantGrepCountTrailer(line)
			break
		}
	case "", "files_with_matches":
		result = strings.TrimSpace(result)
		if result != "" && result != "No files found" {
			lines := strings.Split(result, "\n")
			count, _ = projectAssistantGrepFilesHeader(strings.TrimSpace(lines[0]))
		}
	}
	return fmt.Sprintf("%d result line(s)", count)
}

func projectAssistantGrepCountTrailer(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 7 ||
		fields[0] != "Found" ||
		fields[2] != "total" ||
		(fields[3] != "occurrence" && fields[3] != "occurrences") ||
		fields[4] != "across" ||
		(fields[6] != "file." && fields[6] != "files.") {
		return 0, false
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count < 0 {
		return 0, false
	}
	files, err := strconv.Atoi(fields[5])
	if err != nil || files < 0 {
		return 0, false
	}
	if (count == 1) != (fields[3] == "occurrence") ||
		(files == 1) != (fields[6] == "file.") {
		return 0, false
	}
	return count, true
}

func projectAssistantGrepFilesHeader(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 ||
		fields[0] != "Found" ||
		(fields[2] != "file" && fields[2] != "files") {
		return 0, false
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count < 0 {
		return 0, false
	}
	return count, (count == 1) == (fields[2] == "file")
}

func summarizeProjectPlanningWorkflowArgs(args map[string]any) string {
	parts := []string{}
	if includeFiles, ok := args["includeFiles"].(bool); ok {
		parts = append(parts, fmt.Sprintf("includeFiles %t", includeFiles))
	}
	if n, ok := projectToolNumber(args["maxFiles"]); ok {
		parts = append(parts, fmt.Sprintf("maxFiles %d", n))
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeWorkspaceMutationResult(decoded map[string]any) string {
	parts := []string{}
	if op := projectToolString(decoded["operation"]); op != "" {
		parts = append(parts, op)
	}
	if path := projectToolString(decoded["path"]); path != "" {
		parts = append(parts, path)
	}
	if size, ok := projectToolNumber(decoded["size"]); ok {
		parts = append(parts, fmt.Sprintf("%d bytes", size))
	}
	if replacements, ok := projectToolNumber(decoded["replacements"]); ok {
		parts = append(parts, fmt.Sprintf("%d replacement(s)", replacements))
	}
	if additions, ok := projectToolNumber(decoded["additions"]); ok {
		parts = append(parts, fmt.Sprintf("+%d", additions))
	}
	if deletions, ok := projectToolNumber(decoded["deletions"]); ok {
		parts = append(parts, fmt.Sprintf("-%d", deletions))
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeProjectPlanningWorkflowResult(decoded map[string]any) string {
	parts := []string{}
	if summary := projectToolString(decoded["summary"]); summary != "" {
		parts = append(parts, summary)
	}
	if steps, ok := decoded["steps"].([]any); ok && len(steps) > 0 {
		parts = append(parts, fmt.Sprintf("%d step(s)", len(steps)))
	}
	if files := projectToolStringList(decoded["files"]); len(files) > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s): %s", len(files), summarizeProjectToolList(files, 5)))
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeProjectReadinessWorkflowResult(decoded map[string]any) string {
	parts := []string{}
	if status := projectToolString(decoded["status"]); status != "" {
		parts = append(parts, "status "+status)
	}
	if checks := projectToolStringList(decoded["recommendedChecks"]); len(checks) > 0 {
		parts = append(parts, "checks "+summarizeProjectToolList(checks, 4))
	}
	if files := projectToolStringList(decoded["files"]); len(files) > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s): %s", len(files), summarizeProjectToolList(files, 5)))
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeProjectRuntimeWorkflowResult(decoded map[string]any) string {
	parts := []string{}
	if status := projectToolString(decoded["status"]); status != "" {
		parts = append(parts, "status "+status)
	}
	if previewURL := projectToolString(decoded["previewURL"]); previewURL != "" {
		parts = append(parts, "preview "+previewURL)
	}
	if blockers := projectToolStringList(decoded["blockers"]); len(blockers) > 0 {
		parts = append(parts, "blockers "+summarizeProjectToolList(blockers, 3))
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeWorkspaceListResult(decoded map[string]any) string {
	files := projectToolObjectPaths(decoded["files"])
	parts := []string{}
	parts = append(parts, fmt.Sprintf("%d path(s)", len(files)))
	if len(files) > 0 {
		parts = append(parts, summarizeProjectToolList(files, 5))
	}
	if projectToolBool(decoded["truncated"]) {
		parts = append(parts, "truncated")
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeWorkspaceReadResult(decoded map[string]any) string {
	parts := []string{}
	if path := projectToolString(decoded["path"]); path != "" {
		parts = append(parts, "file "+path)
	}
	if size, ok := projectToolNumber(decoded["size"]); ok {
		parts = append(parts, fmt.Sprintf("%d bytes", size))
	}
	if projectToolBool(decoded["binary"]) {
		parts = append(parts, "binary")
	}
	if projectToolBool(decoded["truncated"]) {
		parts = append(parts, "truncated")
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func summarizeWorkspaceSearchResult(decoded map[string]any) string {
	parts := []string{}
	if total, ok := projectToolNumber(decoded["totalCount"]); ok {
		parts = append(parts, fmt.Sprintf("%d match(es)", total))
	}
	paths := projectToolObjectPaths(decoded["results"])
	if len(paths) > 0 {
		parts = append(parts, summarizeProjectToolList(paths, 5))
	}
	if projectToolBool(decoded["incomplete"]) {
		parts = append(parts, "incomplete")
	}
	if projectToolBool(decoded["truncated"]) {
		parts = append(parts, "truncated")
	}
	return truncateProjectToolInfo(strings.Join(parts, "; "))
}

func projectToolCallResultStatus(name, result string) string {
	baseName := projectToolBaseName(name)
	if baseName != projectToolCommitFiles && baseName != projectToolCommitProjectFiles {
		return "succeeded"
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result)), &decoded); err != nil {
		return "succeeded"
	}
	switch strings.ToLower(projectToolString(decoded["phase"])) {
	case "pending", "running":
		return "running"
	case "failed":
		return "failed"
	default:
		return "succeeded"
	}
}

func projectToolBaseName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if idx := strings.LastIndex(name, "__"); idx >= 0 {
		return name[idx+2:]
	}
	return name
}

func projectToolString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func projectToolRawString(value any) (string, bool) {
	v, ok := value.(string)
	return v, ok
}

func projectToolFilePaths(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	paths := make([]string, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if path := projectToolString(obj["path"]); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func projectToolStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := projectToolString(item); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func projectToolObjectPaths(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if path := projectToolString(obj["path"]); path != "" {
			out = append(out, path)
		}
	}
	return out
}

func projectToolNumber(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func projectToolInt(value any) int {
	n, ok := projectToolNumber(value)
	if !ok || n <= 0 {
		return 0
	}
	if n > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(n)
}

func projectToolBool(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

func summarizeProjectToolList(values []string, limit int) string {
	if len(values) == 0 {
		return ""
	}
	if limit <= 0 || len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:limit], ", ") + fmt.Sprintf(", +%d more", len(values)-limit)
}

func truncateProjectToolInfo(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= projectToolInfoLimit {
		return value
	}
	if projectToolInfoLimit <= 3 {
		return value[:projectToolInfoLimit]
	}
	return strings.TrimSpace(value[:projectToolInfoLimit-3]) + "..."
}
