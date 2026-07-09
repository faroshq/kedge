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

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/store"
)

const (
	// projectAssistantHistoryEvidenceMaxTurns bounds how many recent assistant
	// turns contribute a tool-activity trail to the reconstructed evidence
	// block. Older turns fall back to the compacted conversation text.
	projectAssistantHistoryEvidenceMaxTurns = 12
	// projectAssistantHistoryEvidenceMaxActions bounds how many individual tool
	// actions are rendered per assistant turn so a single scaffolding turn that
	// wrote many files cannot dominate the block.
	projectAssistantHistoryEvidenceMaxActions = 8
	// projectAssistantHistoryEvidenceMaxBytes caps the whole evidence block.
	projectAssistantHistoryEvidenceMaxBytes = 4000
)

// projectAssistantHistoryToolEvidence reconstructs a compact, model-facing
// record of what earlier assistant turns actually did — which files they read,
// edited, committed, or which templates and infrastructure they inspected —
// from the persisted assistantActions metadata.
//
// The cross-turn prompt assembly otherwise replays only user/assistant prose,
// so without this the model forgets it already read a file (and re-reads it) or
// hallucinates file contents it never saw this turn. The trail carries tool
// names, summarized arguments (paths/queries/counts), and summarized results —
// never raw file contents or secrets, matching the tool-disclosure boundary.
func projectAssistantHistoryToolEvidence(history []store.Message) string {
	if len(history) == 0 {
		return ""
	}
	type turnTrail struct {
		lines []string
	}
	var trails []turnTrail
	for _, m := range history {
		if m.Role != aiv1alpha1.ProjectMessageRoleAssistant {
			continue
		}
		actions := projectAssistantUIActionsFromMetadata(m.Metadata[projectMessageMetadataAssistantActions])
		if len(actions) == 0 {
			continue
		}
		var lines []string
		for _, a := range actions {
			line := projectAssistantHistoryEvidenceActionLine(a)
			if line == "" {
				continue
			}
			lines = append(lines, line)
			if len(lines) >= projectAssistantHistoryEvidenceMaxActions {
				break
			}
		}
		if len(lines) == 0 {
			continue
		}
		trails = append(trails, turnTrail{lines: lines})
	}
	if len(trails) == 0 {
		return ""
	}
	// Keep only the most recent turns; older tool activity is already folded
	// into the compacted conversation text and the live session snapshot.
	if len(trails) > projectAssistantHistoryEvidenceMaxTurns {
		trails = trails[len(trails)-projectAssistantHistoryEvidenceMaxTurns:]
	}
	var b strings.Builder
	b.WriteString("Tool activity from earlier turns in this project (most recent last). ")
	b.WriteString("Treat this as evidence of work you already performed: do not re-read a file you already read unless it may have changed, and do not claim you read or changed a file that is not listed here or in the live project snapshot.\n")
	for i, trail := range trails {
		b.WriteString("Turn ")
		b.WriteString(projectAssistantHistoryEvidenceOrdinal(len(trails) - i))
		b.WriteString(" ago:\n")
		for _, line := range trail.lines {
			b.WriteString("  - ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		if b.Len() > projectAssistantHistoryEvidenceMaxBytes {
			break
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if len(out) > projectAssistantHistoryEvidenceMaxBytes {
		out = strings.TrimSpace(out[:projectAssistantHistoryEvidenceMaxBytes]) + "\n  … (older tool activity omitted)"
	}
	return out
}

func projectAssistantHistoryEvidenceActionLine(a projectAssistantUIAction) string {
	name := strings.TrimSpace(a.Tool)
	if name == "" {
		name = strings.TrimSpace(a.Label)
	}
	if name == "" {
		name = strings.TrimSpace(a.Kind)
	}
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(name)
	if args := strings.TrimSpace(a.Arguments); args != "" {
		b.WriteString("(")
		b.WriteString(truncateProjectToolInfo(args))
		b.WriteString(")")
	}
	status := strings.TrimSpace(a.Status)
	if status == "failed" {
		b.WriteString(" [failed]")
	}
	detail := strings.TrimSpace(a.Detail)
	if detail == "" {
		detail = strings.TrimSpace(a.Summary)
	}
	if detail != "" {
		b.WriteString(" → ")
		b.WriteString(truncateProjectToolInfo(detail))
	}
	return strings.TrimSpace(b.String())
}

func projectAssistantHistoryEvidenceOrdinal(n int) string {
	switch n {
	case 1:
		return "1 turn"
	default:
		return projectAssistantHistoryEvidenceItoa(n) + " turns"
	}
}

func projectAssistantHistoryEvidenceItoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
