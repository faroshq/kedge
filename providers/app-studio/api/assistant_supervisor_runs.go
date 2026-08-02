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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	asclient "github.com/faroshq/provider-app-studio/client"
	"github.com/faroshq/provider-app-studio/store"
	"github.com/faroshq/provider-app-studio/workspace"
	"github.com/google/uuid"
	"k8s.io/klog/v2"
)

// The durable-run engine behind the assistant HTTP surface: starting runs
// durably per mode, persisting run metadata, the background worker that
// executes a run to its terminal state, and the orphan reconcilers that
// recover runs and work items whose worker died.

func (s *Server) startProjectAssistantRunDurably(ctx context.Context, scope store.Scope, actor, content, clientRequestID string, start func(store.AssistantRun, store.Message, bool) error) (projectAssistantDurableStartResult, error) {
	return s.startProjectAssistantRunDurablyWithMode(ctx, scope, actor, content, clientRequestID, store.AssistantRunModeDiscussion, start)
}

func (s *Server) startProjectAssistantAdaptiveRunDurably(ctx context.Context, scope store.Scope, actor, content, clientRequestID string, start func(store.AssistantRun, store.Message, bool) error) (projectAssistantDurableStartResult, error) {
	return s.startProjectAssistantRunDurablyWithMode(ctx, scope, actor, content, clientRequestID, store.AssistantRunModeAdaptive, start)
}

func (s *Server) startProjectAssistantRunDurablyWithMode(ctx context.Context, scope store.Scope, actor, content, clientRequestID string, mode store.AssistantRunMode, start func(store.AssistantRun, store.Message, bool) error) (projectAssistantDurableStartResult, error) {
	content = strings.TrimSpace(content)
	clientRequestID = strings.TrimSpace(clientRequestID)
	actor = strings.TrimSpace(actor)
	if content == "" || clientRequestID == "" || actor == "" {
		return projectAssistantDurableStartResult{}, newValidationError("content, clientRequestID, and actor are required")
	}
	if err := s.reconcileOrphanedProjectAssistantRun(ctx, scope); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	if prior, err := s.store.FindAssistantRunByClientRequestID(ctx, scope, clientRequestID); err == nil {
		if err := validateProjectAssistantStartReplay(prior, actor, content, mode, "", 0); err != nil {
			return projectAssistantDurableStartResult{}, err
		}
		return projectAssistantDurableStartResult{Run: prior}, nil
	} else if !errors.Is(err, store.ErrAssistantRunNotFound) {
		return projectAssistantDurableStartResult{}, err
	}
	supervisor := s.projectAssistantSupervisor()
	releaseReservation, err := supervisor.Reserve(scope)
	if err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	defer releaseReservation()
	messages, err := s.store.ListMessages(ctx, scope, 1, "")
	if err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	transcriptEmpty := len(messages.Items) == 0
	now := time.Now().UTC()
	assistantAt := now.Add(time.Microsecond)
	user := store.Message{ID: newMessageID(), Role: aiv1alpha1.ProjectMessageRoleUser, ActorID: actor, Content: content, CreatedAt: now, UpdatedAt: now}
	assistant := store.Message{ID: newMessageID(), Role: aiv1alpha1.ProjectMessageRoleAssistant, CreatedAt: assistantAt, UpdatedAt: assistantAt}
	run := store.AssistantRun{ID: "run-" + uuid.NewString(), Mode: mode, Status: store.AssistantRunStatusRunning, ClientRequestID: clientRequestID, UserMessageID: user.ID, ActiveMessageID: assistant.ID, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.captureProjectAssistantApprovalMode(ctx, scope, actor, &run); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	if err := bindProjectAssistantStartRequest(&run, actor, content, "", 0); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	assistant.Metadata = projectAssistantDurableMetadataForTransition(run, "Working", false, false, nil, nil)
	created, err := s.store.CreateAssistantRun(ctx, scope, user, assistant, run)
	if err != nil {
		if prior, ok := s.recoverProjectAssistantStartReplay(ctx, scope, err, clientRequestID, actor, content, mode, "", 0); ok {
			return projectAssistantDurableStartResult{Run: prior}, nil
		}
		return projectAssistantDurableStartResult{}, err
	}
	if created.ID != run.ID {
		if err := validateProjectAssistantStartReplay(created, actor, content, mode, "", 0); err != nil {
			return projectAssistantDurableStartResult{}, err
		}
		return projectAssistantDurableStartResult{Run: created}, nil
	}
	if err := start(created, assistant, transcriptEmpty); err != nil {
		return projectAssistantDurableStartResult{Run: created, User: user, Assistant: assistant}, err
	}
	return projectAssistantDurableStartResult{Run: created, User: user, Assistant: assistant, Started: true}, nil
}

// startProjectAssistantBuildRunDurably creates the WorkItem, its root message,
// assistant placeholder, and initial mutation run in the store's single
// atomic boundary.  The actor is persisted with the root message and WorkItem;
// it is never inferred from prompt text or a client-provided field.
func (s *Server) startProjectAssistantBuildRunDurably(ctx context.Context, scope store.Scope, actor, content, clientRequestID string, start func(store.AssistantRun, store.Message, bool) error) (projectAssistantDurableStartResult, error) {
	return s.startProjectAssistantBuildRunDurablyWithInitialBootstrap(ctx, scope, actor, content, clientRequestID, false, start)
}

func (s *Server) startProjectAssistantBuildRunDurablyWithInitialBootstrap(ctx context.Context, scope store.Scope, actor, content, clientRequestID string, initialBootstrap bool, start func(store.AssistantRun, store.Message, bool) error) (projectAssistantDurableStartResult, error) {
	content, clientRequestID, actor = strings.TrimSpace(content), strings.TrimSpace(clientRequestID), strings.TrimSpace(actor)
	if content == "" || clientRequestID == "" || actor == "" {
		return projectAssistantDurableStartResult{}, newValidationError("content, clientRequestID, and actor are required")
	}
	if err := s.reconcileOrphanedProjectAssistantRun(ctx, scope); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	if prior, err := s.store.FindAssistantRunByClientRequestID(ctx, scope, clientRequestID); err == nil {
		if err := validateProjectAssistantStartReplay(prior, actor, content, store.AssistantRunModeNew, "", 0, initialBootstrap); err != nil {
			return projectAssistantDurableStartResult{}, err
		}
		return projectAssistantDurableStartResult{Run: prior}, nil
	} else if !errors.Is(err, store.ErrAssistantRunNotFound) {
		return projectAssistantDurableStartResult{}, err
	}
	supervisor := s.projectAssistantSupervisor()
	releaseReservation, err := supervisor.Reserve(scope)
	if err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	defer releaseReservation()
	messages, err := s.store.ListMessages(ctx, scope, 1, "")
	if err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	transcriptEmpty := len(messages.Items) == 0
	now := time.Now().UTC()
	assistantAt := now.Add(time.Microsecond)
	user := store.Message{ID: newMessageID(), Role: aiv1alpha1.ProjectMessageRoleUser, ActorID: actor, Content: content, CreatedAt: now, UpdatedAt: now}
	assistant := store.Message{ID: newMessageID(), Role: aiv1alpha1.ProjectMessageRoleAssistant, CreatedAt: assistantAt, UpdatedAt: assistantAt}
	item := store.AssistantWorkItem{ID: "work-item-" + uuid.NewString(), RootMessageID: user.ID, CreatedBy: actor, Status: store.AssistantWorkItemStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
	user.WorkItemID = item.ID
	assistant.WorkItemID = item.ID
	run := store.AssistantRun{ID: "run-" + uuid.NewString(), WorkItemID: item.ID, Mode: store.AssistantRunModeNew, Status: store.AssistantRunStatusRunning, ClientRequestID: clientRequestID, UserMessageID: user.ID, ActiveMessageID: assistant.ID, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.captureProjectAssistantApprovalMode(ctx, scope, actor, &run); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	if err := bindProjectAssistantStartRequest(&run, actor, content, "", 0, initialBootstrap); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	assistant.Metadata = projectAssistantDurableMetadataForTransition(run, "Working", false, false, nil, nil)
	if initialBootstrap && transcriptEmpty {
		assistant.Metadata[projectAssistantMetadataInitialBuild] = true
	}
	if _, err := s.store.CreateWorkItemAndAssistantRun(ctx, scope, item, user, assistant, run); err != nil {
		if prior, ok := s.recoverProjectAssistantStartReplay(ctx, scope, err, clientRequestID, actor, content, store.AssistantRunModeNew, "", 0, initialBootstrap); ok {
			return projectAssistantDurableStartResult{Run: prior}, nil
		}
		return projectAssistantDurableStartResult{}, err
	}
	if err := start(run, assistant, transcriptEmpty); err != nil {
		return projectAssistantDurableStartResult{Run: run, User: user, Assistant: assistant}, err
	}
	return projectAssistantDurableStartResult{Run: run, User: user, Assistant: assistant, Started: true}, nil
}

// startProjectAssistantContinueRunDurably is the only HTTP-to-store boundary
// that may reactivate a suspended WorkItem. The store repeats all selection
// checks while atomically creating the next messages and run.
func (s *Server) startProjectAssistantContinueRunDurably(ctx context.Context, scope store.Scope, workItemID, actor string, expectedRevision int64, content, clientRequestID string, start func(store.AssistantRun, store.Message, bool) error) (projectAssistantDurableStartResult, error) {
	content, clientRequestID, actor, workItemID = strings.TrimSpace(content), strings.TrimSpace(clientRequestID), strings.TrimSpace(actor), strings.TrimSpace(workItemID)
	if content == "" || clientRequestID == "" || actor == "" || workItemID == "" || expectedRevision < 1 {
		return projectAssistantDurableStartResult{}, newValidationError("content, clientRequestID, actor, workItemID, and workItemRevision are required")
	}
	if err := s.reconcileOrphanedProjectAssistantRun(ctx, scope); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	if prior, err := s.store.FindAssistantRunByClientRequestID(ctx, scope, clientRequestID); err == nil {
		if err := validateProjectAssistantStartReplay(prior, actor, content, store.AssistantRunModeContinue, workItemID, expectedRevision); err != nil {
			return projectAssistantDurableStartResult{}, err
		}
		return projectAssistantDurableStartResult{Run: prior}, nil
	} else if !errors.Is(err, store.ErrAssistantRunNotFound) {
		return projectAssistantDurableStartResult{}, err
	}
	supervisor := s.projectAssistantSupervisor()
	releaseReservation, err := supervisor.Reserve(scope)
	if err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	defer releaseReservation()
	item, err := s.store.GetAssistantWorkItem(ctx, scope, workItemID)
	if err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	now := time.Now().UTC()
	assistantAt := now.Add(time.Microsecond)
	user := store.Message{ID: newMessageID(), Role: aiv1alpha1.ProjectMessageRoleUser, ActorID: actor, WorkItemID: workItemID, Content: content, CreatedAt: now, UpdatedAt: now}
	assistant := store.Message{ID: newMessageID(), Role: aiv1alpha1.ProjectMessageRoleAssistant, WorkItemID: workItemID, CreatedAt: assistantAt, UpdatedAt: assistantAt}
	run := store.AssistantRun{ID: "run-" + uuid.NewString(), WorkItemID: workItemID, Mode: store.AssistantRunModeContinue, ExpectedGrantRevision: item.GrantRevision, Status: store.AssistantRunStatusRunning, ClientRequestID: clientRequestID, UserMessageID: user.ID, ActiveMessageID: assistant.ID, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.captureProjectAssistantApprovalMode(ctx, scope, actor, &run); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	if err := bindProjectAssistantStartRequest(&run, actor, content, workItemID, expectedRevision); err != nil {
		return projectAssistantDurableStartResult{}, err
	}
	assistant.Metadata = projectAssistantDurableMetadataForTransition(run, "Working", false, false, nil, nil)
	if _, err := s.store.ResumeWorkItemAndCreateAssistantRun(ctx, scope, workItemID, actor, expectedRevision, user, assistant, run); err != nil {
		if prior, ok := s.recoverProjectAssistantStartReplay(ctx, scope, err, clientRequestID, actor, content, store.AssistantRunModeContinue, workItemID, expectedRevision); ok {
			return projectAssistantDurableStartResult{Run: prior}, nil
		}
		return projectAssistantDurableStartResult{}, err
	}
	if err := start(run, assistant, false); err != nil {
		return projectAssistantDurableStartResult{Run: run, User: user, Assistant: assistant}, err
	}
	return projectAssistantDurableStartResult{Run: run, User: user, Assistant: assistant, Started: true}, nil
}

type projectAssistantSupervisorRunContextKey struct{}

const (
	projectAssistantMetadataRunID                = "assistantRunID"
	projectAssistantMetadataRevision             = "assistantRevision"
	projectAssistantMetadataWorkingStatus        = "assistantStatus"
	projectAssistantMetadataProvisional          = "assistantProvisional"
	projectAssistantMetadataPreviewRefreshNeeded = "previewRefreshNeeded"
	projectAssistantMetadataPlan                 = "assistantPlan"
	projectAssistantMetadataInitialBuild         = "assistantInitialBuild"
	projectAssistantMetadataProgress             = "assistantProgress"
	projectAssistantProgressMaxMessages          = 32
	projectAssistantWorkedDurationMaxMS          = int64((7 * 24 * time.Hour) / time.Millisecond)
	projectAssistantTraceMaxSequence             = 10_000
)

type projectAssistantProgressSnapshot struct {
	Version          int      `json:"version"`
	Messages         []string `json:"messages"`
	MessageSequences []int    `json:"messageSequences,omitempty"`
	WorkedDurationMS int64    `json:"workedDurationMs"`
}

func projectAssistantDurableMetadataForTransition(run store.AssistantRun, status string, provisional, preview bool, toolCalls []projectToolCallStreamEvent, plan *projectAssistantPlanSnapshot) map[string]any {
	metadata := projectAssistantMessageMetadata(status, sanitizeProjectToolCallStreamEventsForMetadata(toolCalls))
	metadata[projectAssistantMetadataRunID] = run.ID
	metadata[projectAssistantMetadataRevision] = run.Revision
	metadata[projectAssistantMetadataWorkingStatus] = status
	metadata[projectAssistantMetadataProvisional] = provisional
	metadata[projectAssistantMetadataPreviewRefreshNeeded] = preview
	if plan, ok := projectAssistantPlanSnapshotFromMetadata(plan); ok {
		metadata[projectAssistantMetadataPlan] = *plan
	}
	return metadata
}

func projectAssistantDurableMetadataFromExisting(run store.AssistantRun, status string, provisional bool, existing map[string]any) map[string]any {
	metadata := map[string]any{}
	if actions := projectAssistantActionFeedFromMetadata(existing[projectMessageMetadataAssistantActionFeed]); len(actions) > 0 {
		metadata[projectMessageMetadataAssistantActionFeed] = actions
	}
	if interrupt := projectAssistantUIInterruptFromMetadata(existing[projectMessageMetadataAssistantInterrupt]); interrupt != nil {
		metadata[projectMessageMetadataAssistantInterrupt] = interrupt
	}
	if plan, ok := projectAssistantPlanSnapshotFromMetadata(existing[projectAssistantMetadataPlan]); ok {
		metadata[projectAssistantMetadataPlan] = *plan
	}
	if initialBuild, _ := existing[projectAssistantMetadataInitialBuild].(bool); initialBuild {
		metadata[projectAssistantMetadataInitialBuild] = true
	}
	if progress, ok := projectAssistantProgressSnapshotFromMetadata(existing[projectAssistantMetadataProgress]); ok {
		metadata[projectAssistantMetadataProgress] = *progress
	}
	preview, _ := existing[projectAssistantMetadataPreviewRefreshNeeded].(bool)
	metadata[projectAssistantMetadataRunID] = run.ID
	metadata[projectAssistantMetadataRevision] = run.Revision
	metadata[projectAssistantMetadataWorkingStatus] = status
	metadata[projectAssistantMetadataProvisional] = provisional
	metadata[projectAssistantMetadataPreviewRefreshNeeded] = preview
	return metadata
}

func projectAssistantProgressSnapshotFromMetadata(value any) (*projectAssistantProgressSnapshot, bool) {
	if value == nil {
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var progress projectAssistantProgressSnapshot
	if err := decoder.Decode(&progress); err != nil ||
		progress.Version != 1 ||
		len(progress.Messages) == 0 ||
		len(progress.Messages) > projectAssistantProgressMaxMessages ||
		progress.WorkedDurationMS < 0 ||
		progress.WorkedDurationMS > projectAssistantWorkedDurationMaxMS {
		return nil, false
	}
	for _, message := range progress.Messages {
		if message == "" ||
			message != strings.TrimSpace(message) ||
			len(message) > projectEinoAssistantProgressMaxBytes ||
			!utf8.ValidString(message) ||
			strings.IndexFunc(message, unicode.IsControl) >= 0 {
			return nil, false
		}
	}
	if len(progress.MessageSequences) > 0 {
		if len(progress.MessageSequences) != len(progress.Messages) {
			return nil, false
		}
		previous := 0
		for _, sequence := range progress.MessageSequences {
			if sequence <= 0 || sequence > projectAssistantTraceMaxSequence {
				return nil, false
			}
			if sequence <= previous {
				return nil, false
			}
			previous = sequence
		}
	}
	return &progress, true
}

// projectAssistantPlanSnapshotFromMetadata is the durable metadata boundary
// for plans. Postgres rehydrates JSON values as generic maps, so decode them
// back into the public snapshot shape and retain only values the write_todos
// producer could have emitted. Validation deliberately does not sanitize or
// redact labels again: a retained plan must preserve its already-sanitized
// user-facing wording exactly.
func projectAssistantPlanSnapshotFromMetadata(value any) (*projectAssistantPlanSnapshot, bool) {
	if value == nil {
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil || !projectAssistantPlanMetadataKeysValid(raw) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var plan projectAssistantPlanSnapshot
	if err := decoder.Decode(&plan); err != nil || !projectAssistantPlanSnapshotValid(plan) {
		return nil, false
	}
	return &plan, true
}

func projectAssistantPlanMetadataKeysValid(raw []byte) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) != 1 {
		return false
	}
	rawSteps, ok := object["steps"]
	if !ok {
		return false
	}
	var steps []map[string]json.RawMessage
	if err := json.Unmarshal(rawSteps, &steps); err != nil {
		return false
	}
	for _, step := range steps {
		if _, ok := step["content"]; !ok {
			return false
		}
		if _, ok := step["status"]; !ok {
			return false
		}
		for key := range step {
			switch key {
			case "content", "activeForm", "status":
			default:
				return false
			}
		}
	}
	return true
}

func projectAssistantPlanSnapshotValid(plan projectAssistantPlanSnapshot) bool {
	if len(plan.Steps) == 0 || len(plan.Steps) > projectEinoAssistantTodoProgressMaxItems {
		return false
	}
	inProgress := 0
	for _, step := range plan.Steps {
		if !projectAssistantPlanLabelValid(step.Content, true) || !projectAssistantPlanLabelValid(step.ActiveForm, false) {
			return false
		}
		switch step.Status {
		case "pending", "completed":
		case "in_progress":
			inProgress++
			if inProgress > 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func projectAssistantPlanLabelValid(label string, required bool) bool {
	if !utf8.ValidString(label) || len(label) > projectEinoAssistantTodoProgressMaxLabelBytes {
		return false
	}
	if projectEinoAssistantTodoProgressLabel(label) != label {
		return false
	}
	return !required || strings.TrimSpace(label) != ""
}

type projectAssistantDurableMetadataState struct {
	status             string
	provisional        bool
	toolCalls          []projectToolCallStreamEvent
	plan               *projectAssistantPlanSnapshot
	initialBuild       bool
	progressMessages   []string
	progressSequences  []int
	actionSequences    map[string]int
	nextTraceSequence  int
	workedDuration     time.Duration
	workSegmentStarted time.Time
}

func (s *projectAssistantDurableMetadataState) appendProgress(message string) bool {
	if s == nil {
		return false
	}
	message, reason := projectEinoAssistantProgressMessage(message)
	if reason != "" {
		return false
	}
	if len(s.progressMessages) > 0 && s.progressMessages[len(s.progressMessages)-1] == message {
		return false
	}
	if len(s.progressMessages) >= projectAssistantProgressMaxMessages {
		return false
	}
	s.progressMessages = append(s.progressMessages, message)
	s.progressSequences = append(s.progressSequences, s.nextSequence())
	return true
}

func (s *projectAssistantDurableMetadataState) restoreTrace(progress *projectAssistantProgressSnapshot, actions []projectAssistantActionFeedItem) {
	if s == nil {
		return
	}
	s.actionSequences = map[string]int{}
	if progress != nil {
		s.progressMessages = append([]string(nil), progress.Messages...)
		if len(progress.MessageSequences) == len(progress.Messages) {
			s.progressSequences = append([]int(nil), progress.MessageSequences...)
		} else {
			s.progressSequences = make([]int, len(progress.Messages))
		}
		for _, sequence := range s.progressSequences {
			if sequence > s.nextTraceSequence {
				s.nextTraceSequence = sequence
			}
		}
		s.workedDuration = time.Duration(progress.WorkedDurationMS) * time.Millisecond
	}
	for _, action := range actions {
		if action.ID == "" || action.Sequence <= 0 || action.Sequence > projectAssistantTraceMaxSequence {
			continue
		}
		s.actionSequences[action.ID] = action.Sequence
		if action.Sequence > s.nextTraceSequence {
			s.nextTraceSequence = action.Sequence
		}
	}
}

func (s *projectAssistantDurableMetadataState) upsertToolCall(event projectToolCallStreamEvent) {
	if s == nil || event.ID == "" {
		return
	}
	// Ordering is owned by the durable callback boundary. Ignore any sequence
	// carried by an upstream event and recover only from server state.
	event.Sequence = 0
	publicID := projectAssistantActionPublicID(event.ID)
	if s.actionSequences != nil {
		event.Sequence = s.actionSequences[publicID]
	}
	if event.Sequence == 0 {
		for _, existing := range s.toolCalls {
			if existing.ID == event.ID && existing.Sequence > 0 {
				event.Sequence = existing.Sequence
				break
			}
		}
	}
	if event.Sequence == 0 {
		event.Sequence = s.nextSequence()
	}
	if event.Sequence > 0 {
		if s.actionSequences == nil {
			s.actionSequences = map[string]int{}
		}
		s.actionSequences[publicID] = event.Sequence
	}
	s.toolCalls = upsertProjectToolCallStreamEvent(s.toolCalls, event)
}

func (s *projectAssistantDurableMetadataState) nextSequence() int {
	if s == nil || s.nextTraceSequence >= projectAssistantTraceMaxSequence {
		return 0
	}
	s.nextTraceSequence++
	return s.nextTraceSequence
}

func (s *projectAssistantDurableMetadataState) progressSnapshot(now time.Time) *projectAssistantProgressSnapshot {
	if s == nil || len(s.progressMessages) == 0 {
		return nil
	}
	duration := s.workedDuration
	if !s.workSegmentStarted.IsZero() && now.After(s.workSegmentStarted) {
		duration += now.Sub(s.workSegmentStarted)
	}
	durationMS := duration.Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	if durationMS > projectAssistantWorkedDurationMaxMS {
		durationMS = projectAssistantWorkedDurationMaxMS
	}
	messageSequences := append([]int(nil), s.progressSequences...)
	if len(messageSequences) != len(s.progressMessages) {
		messageSequences = nil
	} else {
		for _, sequence := range messageSequences {
			if sequence <= 0 {
				messageSequences = nil
				break
			}
		}
	}
	return &projectAssistantProgressSnapshot{
		Version:          1,
		Messages:         append([]string(nil), s.progressMessages...),
		MessageSequences: messageSequences,
		WorkedDurationMS: durationMS,
	}
}

func projectAssistantRunDisplayStatus(status store.AssistantRunStatus, fallback string) string {
	switch status {
	case store.AssistantRunStatusCompleted:
		return "Completed"
	case store.AssistantRunStatusAborted:
		return "Aborted"
	case store.AssistantRunStatusFailed:
		return "Failed"
	case store.AssistantRunStatusInterrupted:
		return "Interrupted"
	case store.AssistantRunStatusPendingPermission:
		return projectMessageStatusPendingPermission
	case store.AssistantRunStatusPendingInput:
		return projectMessageStatusPendingInput
	}
	return fallback
}

// persistProjectAssistantDurableMetadata is the one metadata write path for
// both a fresh run and a resumed continuation. It derives the metadata revision
// from the same transition that persists the run and message.
func (s *Server) persistProjectAssistantDurableMetadata(ctx context.Context, accumulator *projectAssistantSnapshotAccumulator, workspaceScope workspace.Scope, state *projectAssistantDurableMetadataState, runStatus *store.AssistantRunStatus) error {
	return accumulator.UpdateSnapshot(ctx, func(run *store.AssistantRun, message *store.Message) {
		if runStatus != nil {
			run.Status = *runStatus
		}
		if assistantRunTerminal(run.Status) {
			state.provisional = false
		}
		next := *run
		next.Revision++
		metadata := projectAssistantDurableMetadataForTransition(
			next,
			projectAssistantRunDisplayStatus(run.Status, state.status),
			state.provisional,
			s.projectAssistantPreviewRefreshNeeded(ctx, workspaceScope, "", false, state.toolCalls),
			state.toolCalls,
			state.plan,
		)
		// Resumed segments begin with durable actions from the previous segment.
		// Keep that history and only upsert new action updates.
		actions := projectAssistantActionFeedFromMetadata(message.Metadata[projectMessageMetadataAssistantActionFeed])
		for _, action := range projectAssistantActionFeedUpdatesFromToolCalls(state.toolCalls) {
			actions = applyProjectAssistantActionFeedUpdate(actions, action)
		}
		if len(actions) > 0 {
			metadata[projectMessageMetadataAssistantActionFeed] = actions
		}
		if preview, _ := message.Metadata[projectAssistantMetadataPreviewRefreshNeeded].(bool); preview {
			metadata[projectAssistantMetadataPreviewRefreshNeeded] = true
		}
		if _, ok := metadata[projectAssistantMetadataPlan]; !ok {
			if plan, ok := projectAssistantPlanSnapshotFromMetadata(message.Metadata[projectAssistantMetadataPlan]); ok {
				metadata[projectAssistantMetadataPlan] = *plan
			}
		}
		if state.initialBuild {
			metadata[projectAssistantMetadataInitialBuild] = true
		} else if initialBuild, _ := message.Metadata[projectAssistantMetadataInitialBuild].(bool); initialBuild {
			metadata[projectAssistantMetadataInitialBuild] = true
		}
		if progress := state.progressSnapshot(time.Now().UTC()); progress != nil {
			metadata[projectAssistantMetadataProgress] = *progress
		} else if progress, ok := projectAssistantProgressSnapshotFromMetadata(message.Metadata[projectAssistantMetadataProgress]); ok {
			metadata[projectAssistantMetadataProgress] = *progress
		}
		message.Metadata = metadata
	})
}

func (s *Server) runProjectAssistantWorker(ctx context.Context, accumulator *projectAssistantSnapshotAccumulator, request *http.Request, id identity, c *asclient.Client, project *aiv1alpha1.Project, run store.AssistantRun, start *projectAssistantStreamStart) {
	content := &strings.Builder{}
	workSegmentStarted := time.Now().UTC()
	state := &projectAssistantDurableMetadataState{
		status: "Working",
		initialBuild: start != nil &&
			start.InitialApprovedPlan != nil &&
			strings.TrimSpace(start.InitialApprovedPlan.Goal) != "",
		workSegmentStarted: workSegmentStarted,
	}
	workspaceScope := projectWorkspaceScope(id, project.Name)
	persistMetadata := func(ctx context.Context, runStatus *store.AssistantRunStatus) error {
		return s.persistProjectAssistantDurableMetadata(ctx, accumulator, workspaceScope, state, runStatus)
	}
	persistWorkItemTerminal := func(ctx context.Context, runStatus store.AssistantRunStatus, itemStatus store.AssistantWorkItemStatus, reason string) error {
		return accumulator.TransitionWorkItemTerminal(ctx, runStatus, itemStatus, reason, func(committed *store.AssistantRun, message *store.Message) {
			state.provisional = false
			initialBuild := state.initialBuild
			if persisted, _ := message.Metadata[projectAssistantMetadataInitialBuild].(bool); persisted {
				initialBuild = true
			}
			message.Metadata = projectAssistantDurableMetadataForTransition(
				*committed,
				projectAssistantRunDisplayStatus(runStatus, state.status),
				false,
				s.projectAssistantPreviewRefreshNeeded(ctx, workspaceScope, "", false, state.toolCalls),
				state.toolCalls,
				state.plan,
			)
			if initialBuild {
				message.Metadata[projectAssistantMetadataInitialBuild] = true
			}
			if progress := state.progressSnapshot(time.Now().UTC()); progress != nil {
				message.Metadata[projectAssistantMetadataProgress] = *progress
			}
		})
	}
	hasDurableWorkItem := func() bool {
		if committed, ok := accumulator.CommittedRun(); ok {
			return strings.TrimSpace(committed.WorkItemID) != ""
		}
		return strings.TrimSpace(run.WorkItemID) != ""
	}
	var snapshotErr error
	var snapshotErrMu sync.Mutex
	recordSnapshotErr := func(err error) {
		if err == nil {
			return
		}
		snapshotErrMu.Lock()
		if snapshotErr == nil {
			snapshotErr = err
		}
		snapshotErrMu.Unlock()
	}
	getSnapshotErr := func() error {
		snapshotErrMu.Lock()
		defer snapshotErrMu.Unlock()
		return snapshotErr
	}
	var callbackMu sync.Mutex
	callbacksClosed := false
	req := request.Clone(context.WithValue(ctx, projectAssistantSupervisorRunContextKey{}, run))
	result, err := s.generateProjectAssistantResultWithStart(req, id, c, project, projectAssistantStreamCallbacks{
		OnChunk: func(chunk string) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed {
				return
			}
			recordSnapshotErr(accumulator.UpdateText(ctx, appendProjectAssistantStreamBlock(content, chunk), false))
		},
		OnProgress: func(message string) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed {
				return
			}
			if state.appendProgress(message) {
				recordSnapshotErr(persistMetadata(ctx, nil))
			}
		},
		// The provisional flag is persisted only when it changes. It fires once
		// per streamed chunk, and an immediate durable snapshot per chunk turns
		// one long answer into hundreds of full run+message+metadata writes and
		// as many SSE revision bumps carrying identical content.
		OnProvisionalText: func(_ string) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed || state.provisional {
				return
			}
			state.provisional = true
			recordSnapshotErr(persistMetadata(ctx, nil))
		},
		OnProvisionalReset: func() {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed || !state.provisional {
				return
			}
			state.provisional = false
			recordSnapshotErr(persistMetadata(ctx, nil))
		},
		OnStatus: func(nextStatus string) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed {
				return
			}
			state.status = nextStatus
			recordSnapshotErr(persistMetadata(ctx, nil))
		},
		OnPlan: func(plan projectAssistantPlanSnapshot) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed {
				return
			}
			state.plan = &plan
			recordSnapshotErr(persistMetadata(ctx, nil))
		},
		OnToolCall: func(event projectToolCallStreamEvent) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed {
				return
			}
			state.upsertToolCall(event)
			recordSnapshotErr(persistMetadata(ctx, nil))
		},
		OnAssistantEvent: func(event projectAssistantEvent) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			if callbacksClosed {
				return
			}
			if event.Permission != nil && event.Permission.ToolCallID != "" {
				state.upsertToolCall(projectToolCallStreamEvent{ID: event.Permission.ToolCallID, Name: event.Permission.ToolName, Status: "permission_required", Summary: event.Permission.Reason, Permission: event.Permission})
			}
			if event.FollowUp != nil && event.FollowUp.ToolCallID != "" {
				state.upsertToolCall(projectToolCallStreamEvent{ID: event.FollowUp.ToolCallID, Name: projectToolAskFollowUp, Status: "input_required", Summary: event.FollowUp.Prompt, FollowUp: event.FollowUp})
			}
			if event.Checkpoint != nil {
				for i := range state.toolCalls {
					if state.toolCalls[i].Status == "permission_required" || state.toolCalls[i].Status == "input_required" {
						state.toolCalls[i].Checkpoint = event.Checkpoint
					}
				}
			}
			recordSnapshotErr(persistMetadata(ctx, nil))
		},
	}, start)
	callbackMu.Lock()
	callbacksClosed = true
	contentText := content.String()
	callbackMu.Unlock()
	state.initialBuild = state.initialBuild || result.InitialBuild
	reply := result.Content
	initialBuildCompletionEnforced := state.initialBuild
	completionSuspensionReason := projectAssistantCompletionSuspensionReason(result, initialBuildCompletionEnforced)
	engineCompleted := err == nil && completionSuspensionReason == ""
	finalContent := projectAssistantDurableTerminalContent(reply, contentText, err)
	if result.CompletionEvidence.SourceMutationRevision > 0 {
		finalContent = projectAssistantMutationTerminalContent(result.CompletionEvidence, engineCompleted)
	}
	recordSnapshotErr(accumulator.UpdateText(ctx, finalContent, true))
	if getSnapshotErr() != nil {
		return
	}
	// A durable Stop wins even if the engine concurrently returns success or
	// an interrupt. Do this before interpreting the engine result so a stopped
	// run cannot become completed or pending again.
	if ctx.Err() != nil {
		state.status = "Aborted"
		runStatus := store.AssistantRunStatusAborted
		var transitionErr error
		if hasDurableWorkItem() {
			transitionErr = persistWorkItemTerminal(context.Background(), runStatus, store.AssistantWorkItemStatusSuspended, "aborted")
		} else {
			_, transitionErr = accumulator.supervisor.AbortWith(projectMessageScope(id.orgUUID, id.workspaceUUID, project), run.ID, nil)
		}
		recordSnapshotErr(transitionErr)
		if transitionErr == nil {
			if committed, ok := accumulator.CommittedRun(); ok {
				accumulator.supervisor.log("aborted", projectMessageScope(id.orgUUID, id.workspaceUUID, project), committed)
			}
		}
		return
	}
	if reason := completionSuspensionReason; reason != "" &&
		(err == nil || projectEinoAssistantBoundedExit(err)) {
		state.status = "Suspended"
		runStatus := store.AssistantRunStatusInterrupted
		recordSnapshotErr(persistWorkItemTerminal(ctx, runStatus, store.AssistantWorkItemStatusSuspended, reason))
		return
	}
	if err == nil {
		if projectAssistantTerminalPlanCompleted(result.CompletionEvidence, state.toolCalls) {
			state.plan = projectAssistantCompletedPlanSnapshot(state.plan)
		}
		state.status = "Completed"
		runStatus := store.AssistantRunStatusCompleted
		var transitionErr error
		if hasDurableWorkItem() {
			transitionErr = persistWorkItemTerminal(ctx, runStatus, store.AssistantWorkItemStatusCompleted, "completed")
		} else {
			transitionErr = persistMetadata(ctx, &runStatus)
		}
		recordSnapshotErr(transitionErr)
		if transitionErr == nil {
			if committed, ok := accumulator.CommittedRun(); ok {
				accumulator.supervisor.log("completed", projectMessageScope(id.orgUUID, id.workspaceUUID, project), committed)
			}
		}
		return
	}
	if errors.Is(err, context.Canceled) {
		state.status = "Aborted"
		runStatus := store.AssistantRunStatusAborted
		var transitionErr error
		if hasDurableWorkItem() {
			transitionErr = persistWorkItemTerminal(context.Background(), runStatus, store.AssistantWorkItemStatusSuspended, "aborted")
		} else {
			transitionErr = persistMetadata(context.Background(), &runStatus)
		}
		recordSnapshotErr(transitionErr)
		if transitionErr == nil {
			if committed, ok := accumulator.CommittedRun(); ok {
				accumulator.supervisor.log("aborted", projectMessageScope(id.orgUUID, id.workspaceUUID, project), committed)
			}
		}
		return
	}
	var permissionErr *projectAssistantPermissionRequiredError
	if errors.As(err, &permissionErr) {
		state.status = projectMessageStatusPendingPermission
		runStatus := store.AssistantRunStatusPendingPermission
		recordSnapshotErr(persistMetadata(context.Background(), &runStatus))
		return
	}
	var inputErr *projectAssistantInputRequiredError
	if errors.As(err, &inputErr) {
		state.status = projectMessageStatusPendingInput
		runStatus := store.AssistantRunStatusPendingInput
		recordSnapshotErr(persistMetadata(context.Background(), &runStatus))
		return
	}
	state.status = "Failed"
	runStatus := store.AssistantRunStatusFailed
	var transitionErr error
	if hasDurableWorkItem() {
		transitionErr = persistWorkItemTerminal(
			context.Background(),
			runStatus,
			store.AssistantWorkItemStatusSuspended,
			projectAssistantWorkItemFailureReason(err),
		)
	} else {
		transitionErr = persistMetadata(context.Background(), &runStatus)
	}
	recordSnapshotErr(transitionErr)
	if transitionErr == nil {
		if committed, ok := accumulator.CommittedRun(); ok {
			accumulator.supervisor.log("failed", projectMessageScope(id.orgUUID, id.workspaceUUID, project), committed)
		}
	}
}

func projectAssistantCompletionSuspensionReason(result projectAssistantRunResult, initialBuild bool) string {
	evidence := result.CompletionEvidence
	mutating := evidence.SourceMutationRevision > 0
	planRequired := initialBuild || evidence.PlanDefined || result.InitialPlan != nil
	if !mutating && !planRequired {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(evidence.VerificationOutcome), "provisioning") {
		return "runtime provisioning"
	}
	if (planRequired && !evidence.PlanComplete) ||
		(mutating && (!evidence.LatestMutationVerified ||
			evidence.VerifiedMutationRevision != evidence.SourceMutationRevision)) ||
		strings.TrimSpace(evidence.VerificationOutcome) != "ready" {
		return "objective incomplete"
	}
	return ""
}

func projectAssistantVerifiedMutationCompleted(evidence projectAssistantCompletionEvidence) bool {
	return evidence.SourceMutationRevision > 0 &&
		evidence.LatestMutationVerified &&
		evidence.VerifiedMutationRevision == evidence.SourceMutationRevision &&
		strings.TrimSpace(evidence.VerificationOutcome) == "ready"
}

func projectAssistantTerminalPlanCompleted(
	evidence projectAssistantCompletionEvidence,
	toolCalls []projectToolCallStreamEvent,
) bool {
	if !projectAssistantVerifiedMutationCompleted(evidence) {
		return false
	}
	if evidence.PlanDefined {
		return evidence.PlanComplete
	}
	for _, call := range toolCalls {
		if projectEinoAssistantCommitTool(call.Name) &&
			strings.TrimSpace(call.Status) == "succeeded" {
			return true
		}
	}
	return false
}

func projectAssistantCompletedPlanSnapshot(
	plan *projectAssistantPlanSnapshot,
) *projectAssistantPlanSnapshot {
	if plan == nil {
		return nil
	}
	completed := cloneProjectAssistantPlanSnapshot(*plan)
	for index := range completed.Steps {
		completed.Steps[index].Status = "completed"
	}
	return &completed
}

func projectAssistantMutationTerminalContent(
	evidence projectAssistantCompletionEvidence,
	engineCompleted bool,
) string {
	runtimeVerified := evidence.SourceMutationRevision > 0 &&
		evidence.LatestMutationVerified &&
		evidence.VerifiedMutationRevision == evidence.SourceMutationRevision &&
		strings.TrimSpace(evidence.VerificationOutcome) == "ready"
	complete := engineCompleted && runtimeVerified && (!evidence.PlanDefined || evidence.PlanComplete)
	status := "Incomplete"
	if complete {
		status = "Complete"
	}
	lines := []string{
		"Status: " + status,
		"",
	}
	switch {
	case complete:
		lines = append(lines,
			"The latest app changes are running in the development preview.",
			"",
			"What I verified:",
			"- The current workspace passed runtime verification.",
		)
	case runtimeVerified:
		lines = append(lines,
			"The app is running in the development preview, but the requested project work is not finished yet.",
			"",
			"What I verified:",
			"- The current workspace passed runtime verification.",
		)
	case strings.EqualFold(strings.TrimSpace(evidence.VerificationOutcome), "provisioning"):
		lines = append(lines,
			"The latest app changes are saved, and the development environment is still starting.",
			"",
			"What is ready:",
			"- Your changes are preserved in the project workspace.",
		)
	default:
		lines = append(lines,
			"The latest app changes are saved, but I could not finish runtime verification yet.",
			"",
			"What is ready:",
			"- Your changes are preserved in the project workspace.",
		)
	}
	if summary := strings.TrimSpace(evidence.VerificationSummary); summary != "" {
		lines = append(lines, "", "What I found:", "- "+strings.ReplaceAll(summary, "\n", " "))
	}
	if len(evidence.Blockers) > 0 {
		lines = append(lines, "", "What is blocking completion:")
		for _, blocker := range evidence.Blockers {
			if blocker = strings.TrimSpace(blocker); blocker != "" {
				lines = append(lines, "- "+strings.ReplaceAll(blocker, "\n", " "))
			}
		}
	}
	if evidence.PlanDefined && !evidence.PlanComplete {
		lines = append(lines, "", "Still to do:", "- Finish the remaining project steps.")
	}
	if !complete {
		next := "Continue from the saved workspace and verify the remaining work."
		switch strings.ToLower(strings.TrimSpace(evidence.VerificationOutcome)) {
		case "provisioning":
			next = "Wait for the development environment to finish starting, then verify again."
		case "not_ready":
			if len(evidence.Blockers) > 0 {
				next = "Resolve the issue above, then run runtime verification again."
			}
		}
		lines = append(lines, "", "Next:", "- "+next)
	}
	return strings.Join(lines, "\n")
}

func projectAssistantWorkItemFailureReason(err error) string {
	if projectEinoAssistantNoProgressExceeded(err) {
		return "no_progress"
	}
	return "failed"
}

func projectAssistantTerminalFailureContent(err error) string {
	if projectEinoAssistantNoProgressExceeded(err) {
		return "I stopped because this run did not make implementation progress within the current phase. Your project changes are preserved. Send another message with what you want me to do next."
	}
	if projectEinoAssistantMaxIterationsExceeded(err) {
		return "I stopped after reaching the bounded action limit. Your project changes are preserved. Send another message with what you want me to do next."
	}
	return "The assistant run stopped before it could finish. Your project changes are preserved. Send another message with what you want me to do next."
}

func projectAssistantContentWithTerminalFailure(content string, err error) string {
	content = strings.TrimSpace(content)
	failure := projectAssistantTerminalFailureContent(err)
	if content == "" {
		return failure
	}
	return content + "\n\n" + failure
}

func projectAssistantShouldPersistTerminalFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var permissionErr *projectAssistantPermissionRequiredError
	if errors.As(err, &permissionErr) {
		return false
	}
	var inputErr *projectAssistantInputRequiredError
	return !errors.As(err, &inputErr)
}

func (s *Server) reconcileOrphanedProjectAssistantRun(ctx context.Context, scope store.Scope) error {
	// Release stranded tasks first. This function only examines the latest run,
	// so on its own it cannot repair a task whose run was superseded — and that
	// is precisely the state that blocks a project indefinitely. A failure here
	// must not stop run recovery, which is the more common repair.
	if err := s.reconcileOrphanedProjectAssistantWorkItems(ctx, scope); err != nil {
		klog.V(2).Infof("app studio work item reconcile failed for project %s: %v", scope.ProjectName, err)
	}
	run, err := s.store.LatestAssistantRun(ctx, scope)
	if err != nil {
		if errors.Is(err, store.ErrAssistantRunNotFound) {
			return nil
		}
		return err
	}
	if run.Status != store.AssistantRunStatusRunning && run.Status != store.AssistantRunStatusStopping {
		return nil
	}
	key := projectAssistantRunKey{OrgUUID: scope.OrgUUID, WorkspaceUUID: scope.WorkspaceUUID, ProjectName: scope.ProjectName, ProjectUID: scope.ProjectUID}
	supervisor := s.projectAssistantSupervisor()
	if supervisor.reserved(scope) {
		return nil
	}
	// Read the active run's ID under the supervisor lock: update() and Stop()
	// mutate active.run while holding it, so comparing after unlocking is a
	// data race, and losing that race marks a live run interrupted.
	supervisor.mu.Lock()
	activeRunID := ""
	if active := supervisor.runs[key]; active != nil {
		activeRunID = active.run.ID
	}
	supervisor.mu.Unlock()
	if activeRunID == run.ID {
		return nil
	}
	// NOTE: ownership is inferred from this process's memory, which is only
	// sound because App Studio runs single-replica (the Helm chart refuses to
	// render otherwise). With a second replica, this pod would see another
	// pod's live run as orphaned and interrupt it mid-mutation. Lifting that
	// limit requires a durable owner lease on the run — owner identity plus a
	// heartbeat, reconciling only runs whose lease has expired — not a wider
	// in-memory check. See docs/app-studio-provider-improvement-plan.md §2.4.
	run.Status = store.AssistantRunStatusInterrupted
	run.UpdatedAt = time.Now().UTC()
	run.Revision++
	message, err := s.findProjectMessage(ctx, scope, run.ActiveMessageID)
	if err != nil {
		return err
	}
	message.UpdatedAt = run.UpdatedAt
	message.Metadata = projectAssistantDurableMetadataFromExisting(run, "Interrupted", false, message.Metadata)
	if run.WorkItemID != "" {
		item, itemErr := s.store.GetAssistantWorkItem(ctx, scope, run.WorkItemID)
		if itemErr != nil {
			return itemErr
		}
		if err := s.store.TransitionWorkItemAndRun(ctx, scope, item.ID, item.Revision, run, store.AssistantWorkItemStatusSuspended, "provider restarted", run.UpdatedAt); err != nil {
			return err
		}
		if err := s.store.AppendMessage(ctx, scope, message); err != nil {
			return err
		}
	} else if err := s.store.SaveAssistantRunSnapshot(ctx, scope, run, []store.Message{message}, run.Revision-1); err != nil {
		return err
	}
	s.projectAssistantSupervisor().log("orphan_interrupted", scope, run)
	return nil
}

func (s *Server) reconcileOrphanedProjectAssistantWorkItems(ctx context.Context, scope store.Scope) error {
	items, err := s.store.ListAssistantWorkItems(ctx, scope)
	if err != nil {
		return err
	}
	supervisor := s.projectAssistantSupervisor()
	if supervisor.reserved(scope) {
		return nil
	}
	for _, item := range items {
		if item.Status != store.AssistantWorkItemStatusActive {
			continue
		}
		runID := strings.TrimSpace(item.ActiveRunID)
		if runID == "" {
			continue
		}
		// A run this process is executing owns its item; never touch it.
		if s.projectAssistantSupervisorRunActive(scope, runID) {
			continue
		}
		run, err := s.store.GetAssistantRun(ctx, scope, runID)
		if errors.Is(err, store.ErrAssistantRunNotFound) {
			// The item points at a run that no longer exists at all.
			if suspendErr := s.suspendOrphanedProjectAssistantWorkItem(ctx, scope, item, store.AssistantRun{}); suspendErr != nil {
				return suspendErr
			}
			continue
		}
		if err != nil {
			return err
		}
		if run.Status == store.AssistantRunStatusRunning || run.Status == store.AssistantRunStatusStopping {
			// Still live, or awaiting the run reconciler above. Leave it.
			continue
		}
		if run.Status == store.AssistantRunStatusPendingPermission || run.Status == store.AssistantRunStatusPendingInput {
			// Waiting on the USER, not on a live goroutine: these states are
			// resumable across restarts (resume rebuilds from the checkpoint;
			// that is what reattachProjectAssistantPendingRun exists for).
			// Suspending them here converted every question the assistant had
			// in flight during a restart into an unanswerable dead card
			// ("assistant run is not waiting for input" on answer submit).
			if err := s.reattachProjectAssistantPendingRun(ctx, scope, run); err != nil {
				klog.V(2).Infof("app studio pending run %s reattach failed for project %s: %v", run.ID, scope.ProjectName, err)
			}
			continue
		}
		if err := s.suspendOrphanedProjectAssistantWorkItem(ctx, scope, item, run); err != nil {
			return err
		}
	}
	return nil
}

// suspendOrphanedProjectAssistantWorkItem moves one stranded task to suspended,
// which is the state a user can continue or discard.
func (s *Server) suspendOrphanedProjectAssistantWorkItem(
	ctx context.Context,
	scope store.Scope,
	item store.AssistantWorkItem,
	run store.AssistantRun,
) error {
	if strings.TrimSpace(run.ID) == "" {
		// The item points at a run that no longer exists, so there is no run
		// revision to advance and nothing valid to transition against.
		return nil
	}
	// Only aborted/failed/interrupted justify suspending a task, so a run that
	// reports completed while its task is still active is recorded as
	// interrupted: it ended without releasing the task, which is not success.
	switch run.Status {
	case store.AssistantRunStatusAborted, store.AssistantRunStatusFailed, store.AssistantRunStatusInterrupted:
	default:
		run.Status = store.AssistantRunStatusInterrupted
	}
	// The store advances the run by exactly one revision on this transition.
	run.Revision++
	run.UpdatedAt = time.Now().UTC()

	err := s.store.TransitionWorkItemAndRun(
		ctx, scope, item.ID, item.Revision, run,
		store.AssistantWorkItemStatusSuspended, "run ended without releasing this task", run.UpdatedAt,
	)
	if err != nil {
		// Losing a race with the owning process is expected and harmless: it
		// means something else already moved the item.
		if errors.Is(err, store.ErrAssistantWorkItemConflict) || errors.Is(err, store.ErrAssistantRunConflict) {
			return nil
		}
		return err
	}
	klog.V(2).Infof("app studio released stranded work item %s in project %s", item.ID, scope.ProjectName)
	return nil
}

// projectAssistantSupervisorRunActive reports whether this process is executing
// the named run for a project.
func (s *Server) projectAssistantSupervisorRunActive(scope store.Scope, runID string) bool {
	supervisor := s.projectAssistantSupervisor()
	key := projectAssistantRunKey{OrgUUID: scope.OrgUUID, WorkspaceUUID: scope.WorkspaceUUID, ProjectName: scope.ProjectName, ProjectUID: scope.ProjectUID}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	active := supervisor.runs[key]
	return active != nil && active.run.ID == runID
}
