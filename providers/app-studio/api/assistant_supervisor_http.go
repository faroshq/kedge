// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/store"
	"github.com/gorilla/mux"
)

type projectAssistantRunStartResponse struct {
	Run       projectAssistantRunView    `json:"run"`
	User      *aiv1alpha1.ProjectMessage `json:"user,omitempty"`
	Assistant aiv1alpha1.ProjectMessage  `json:"assistant"`
}

// projectAssistantRunView is the public run contract. Durable execution
// records also contain checkpoint, audit, grant, and project-scoping state that
// must remain server-side.
type projectAssistantRunView struct {
	ID              string                      `json:"id"`
	WorkItemID      string                      `json:"workItemID,omitempty"`
	Mode            store.AssistantRunMode      `json:"mode,omitempty"`
	ApprovalMode    store.AssistantApprovalMode `json:"approvalMode,omitempty"`
	Status          store.AssistantRunStatus    `json:"status"`
	ClientRequestID string                      `json:"clientRequestID,omitempty"`
	UserMessageID   string                      `json:"userMessageID,omitempty"`
	ActiveMessageID string                      `json:"activeMessageID,omitempty"`
	Revision        int64                       `json:"revision,omitempty"`
	RequestID       string                      `json:"requestID,omitempty"`
	CreatedAt       time.Time                   `json:"createdAt"`
	UpdatedAt       time.Time                   `json:"updatedAt"`
}

type projectAssistantRunSnapshotResponse struct {
	Run     projectAssistantRunView   `json:"run"`
	Message aiv1alpha1.ProjectMessage `json:"message"`
}

func projectAssistantRunToAPI(run store.AssistantRun) projectAssistantRunView {
	return projectAssistantRunView{
		ID:              run.ID,
		WorkItemID:      run.WorkItemID,
		Mode:            run.Mode,
		ApprovalMode:    run.ApprovalMode,
		Status:          run.Status,
		ClientRequestID: run.ClientRequestID,
		UserMessageID:   run.UserMessageID,
		ActiveMessageID: run.ActiveMessageID,
		Revision:        run.Revision,
		RequestID:       run.RequestID,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
}

func projectAssistantRunSnapshotToAPI(snapshot projectAssistantRunSnapshot) projectAssistantRunSnapshotResponse {
	return projectAssistantRunSnapshotResponse{
		Run:     projectAssistantRunToAPI(snapshot.Run),
		Message: projectMessageToAPI(snapshot.Message),
	}
}

type projectAssistantDurableStartResult struct {
	Run       store.AssistantRun
	User      store.Message
	Assistant store.Message
	Started   bool
}

// projectAssistantDurableFinalContent makes the engine's returned response
// authoritative when present. Chunk callbacks are progressive UI snapshots;
// they can be empty or partial and must never truncate or duplicate the final
// durable assistant message.
func projectAssistantDurableFinalContent(reply, streamed string) string {
	return projectAssistantStoredContent(reply, streamed)
}

func projectAssistantDurableTerminalContent(reply, streamed string, err error) string {
	content := projectAssistantDurableFinalContent(reply, streamed)
	if projectEinoAssistantBoundedExit(err) || errors.Is(err, errProjectAssistantNoOutput) {
		if projectEinoAssistantBoundedClosingAnswerValid(content) {
			return content
		}
		if errors.Is(err, errProjectAssistantNoOutput) {
			return projectEinoAssistantBoundedClosingFallback("No usable assistant response was produced for this turn.")
		}
		return projectEinoAssistantBoundedClosingFallback("")
	}
	if projectAssistantShouldPersistTerminalFailure(err) {
		return projectAssistantContentWithTerminalFailure(content, err)
	}
	return content
}

// appendProjectAssistantStreamBlock keeps complete assistant updates readable
// while a tool-driven turn is running. These are accepted assistant prose
// blocks, not token deltas or reasoning content; the final returned response
// remains authoritative for the durable terminal message.
func appendProjectAssistantStreamBlock(content *strings.Builder, block string) string {
	block = strings.TrimSpace(block)
	if block == "" {
		return content.String()
	}
	current := strings.TrimSpace(content.String())
	if current == block || strings.HasSuffix(current, "\n\n"+block) {
		return content.String()
	}
	if content.Len() > 0 {
		content.WriteString("\n\n")
	}
	content.WriteString(block)
	return content.String()
}

// startProjectAssistantRunDurably is the one start boundary for every new
// conversation turn. It validates its durable inputs, reserves the project,
// atomically creates the user message, assistant placeholder and run, then
// hands the run to a server-owned worker. It deliberately accepts no response
// writer and never derives execution from the caller's request context.
func (s *Server) startProjectAssistantRun(w http.ResponseWriter, r *http.Request) {
	c, id, project, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	var request CreateProjectMessageRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Content = strings.TrimSpace(request.Content)
	request.ClientRequestID = strings.TrimSpace(request.ClientRequestID)
	if request.Content == "" || request.ClientRequestID == "" {
		writeProjectError(w, newValidationError("content and clientRequestID are required"))
		return
	}
	scope := projectMessageScope(id.orgUUID, id.workspaceUUID, project)
	supervisor := s.projectAssistantSupervisor()
	action, err := request.assistantAction()
	if err != nil {
		writeProjectError(w, err)
		return
	}
	initialBootstrap, err := s.consumeProjectInitialBootstrap(r.Context(), scope, id.user, request.Content, request.ClientRequestID)
	if err != nil {
		if errors.Is(err, store.ErrProjectBootstrapPermitConflict) {
			writeStatus(w, http.StatusConflict, "Conflict", "initial project bootstrap is already reserved")
			return
		}
		writeProjectError(w, err)
		return
	}
	if initialBootstrap {
		action = projectAssistantActionBuild
	}
	startWorker := func(created store.AssistantRun, assistant store.Message, start *projectAssistantStreamStart) error {
		return supervisor.Start(r.Context(), scope, created, assistant, func(ctx context.Context, accumulator *projectAssistantSnapshotAccumulator) {
			s.runProjectAssistantWorker(ctx, accumulator, r, id, c, project, created, start)
		})
	}
	var started projectAssistantDurableStartResult
	switch action {
	case projectAssistantActionAuto:
		started, err = s.startProjectAssistantAdaptiveRunDurably(r.Context(), scope, id.user, request.Content, request.ClientRequestID, func(created store.AssistantRun, assistant store.Message, _ bool) error {
			return startWorker(created, assistant, nil)
		})
	case projectAssistantActionAsk:
		started, err = s.startProjectAssistantRunDurably(r.Context(), scope, id.user, request.Content, request.ClientRequestID, func(created store.AssistantRun, assistant store.Message, _ bool) error {
			return startWorker(created, assistant, nil)
		})
	case projectAssistantActionBuild:
		started, err = s.startProjectAssistantBuildRunDurablyWithInitialBootstrap(r.Context(), scope, id.user, request.Content, request.ClientRequestID, initialBootstrap, func(created store.AssistantRun, assistant store.Message, transcriptEmpty bool) error {
			var start *projectAssistantStreamStart
			if initialBootstrap && transcriptEmpty {
				plan := projectAssistantInitialCreationPlan(request.Content)
				start = &projectAssistantStreamStart{InitialApprovedPlan: cloneProjectAssistantApprovedPlan(&plan)}
			}
			return startWorker(created, assistant, start)
		})
	case projectAssistantActionContinue:
		started, err = s.startProjectAssistantContinueRunDurably(r.Context(), scope, request.WorkItemID, id.user, request.WorkItemRevision, request.Content, request.ClientRequestID, func(created store.AssistantRun, assistant store.Message, _ bool) error {
			return startWorker(created, assistant, nil)
		})
	}
	if err != nil {
		if errors.Is(err, store.ErrAssistantRunConflict) {
			if _, latestErr := s.store.LatestAssistantRun(r.Context(), scope); latestErr == nil {
				s.writeProjectAssistantRunConflict(w, r.Context(), scope, id.user)
			} else {
				writeStatus(w, http.StatusConflict, "Conflict", "assistant run start is already in progress")
			}
			return
		}
		if errors.Is(err, store.ErrAssistantWorkItemConflict) {
			// "assistant work item conflict" tells the caller nothing they can
			// act on, so both users and the model invented remedies that do not
			// exist ("clear the pending work item" — there is no such control
			// while the item is active). Name the two reachable actions.
			writeStatus(w, http.StatusConflict, "Conflict", s.projectAssistantWorkItemConflictMessage(r.Context(), scope, id.user))
			return
		}
		writeProjectError(w, err)
		return
	}
	s.writeProjectAssistantRunStart(w, http.StatusAccepted, scope, started.Run)
}

// projectAssistantWorkItemConflictMessage explains a blocked start in terms of
// what the caller can actually do about it.
//
// A project allows one active WorkItem at a time, so a second edit plan is
// refused while one is open. The remedy depends on the blocking item's state,
// and getting it wrong wastes the user's time: an ACTIVE item cannot be
// cancelled or discarded at all (cancelProjectAssistantWorkItem requires
// suspended + no active run, and the portal only lists suspended items), so
// telling anyone to "clear" it sends them looking for a control that is not
// there. Stopping the run is what suspends it; continuing is usually what the
// caller actually wants, since the open item carries the plan and its
// authority.
func (s *Server) writeProjectAssistantRunStart(w http.ResponseWriter, status int, scope store.Scope, run store.AssistantRun) {
	message, err := s.findProjectMessage(context.Background(), scope, run.ActiveMessageID)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	response := projectAssistantRunStartResponse{Run: projectAssistantRunToAPI(run), Assistant: projectMessageToAPI(message)}
	if strings.TrimSpace(run.UserMessageID) != "" {
		user, err := s.findProjectMessage(context.Background(), scope, run.UserMessageID)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		apiUser := projectMessageToAPI(user)
		response.User = &apiUser
	}
	writeJSON(w, status, response)
}

func (s *Server) writeProjectAssistantRunConflict(w http.ResponseWriter, ctx context.Context, scope store.Scope, actor string) {
	run, err := s.store.LatestAssistantRun(ctx, scope)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	if s.authorizeProjectAssistantRunActor(ctx, scope, run, actor, false) != nil {
		writeStatus(w, http.StatusConflict, "Conflict", "another assistant run is active")
		return
	}
	writeJSON(w, http.StatusConflict, projectAssistantRunSnapshotResponse{Run: projectAssistantRunToAPI(run)})
}

func (s *Server) latestProjectAssistantRun(w http.ResponseWriter, r *http.Request) {
	_, id, project, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	scope := projectMessageScope(id.orgUUID, id.workspaceUUID, project)
	if err := s.reconcileOrphanedProjectAssistantRun(r.Context(), scope); err != nil {
		writeProjectError(w, err)
		return
	}
	run, err := s.store.LatestAssistantRun(r.Context(), scope)
	if errors.Is(err, store.ErrAssistantRunNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeProjectError(w, err)
		return
	}
	if s.authorizeProjectAssistantRunActor(r.Context(), scope, run, id.user, false) != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	message, err := s.findProjectMessage(r.Context(), scope, run.ActiveMessageID)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectAssistantRunSnapshotToAPI(projectAssistantRunSnapshot{Run: run, Message: message}))
}

func (s *Server) streamProjectAssistantSnapshots(w http.ResponseWriter, r *http.Request) {
	_, id, project, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	scope := projectMessageScope(id.orgUUID, id.workspaceUUID, project)
	runID := mux.Vars(r)["run"]
	if err := s.reconcileOrphanedProjectAssistantRun(r.Context(), scope); err != nil {
		writeProjectError(w, err)
		return
	}
	run, err := s.store.GetAssistantRun(r.Context(), scope, runID)
	if err != nil || s.authorizeProjectAssistantRunActor(r.Context(), scope, run, id.user, false) != nil {
		writeStatus(w, http.StatusNotFound, "NotFound", "assistant run not found")
		return
	}
	after := projectAssistantAfterRevision(r)
	updates, unsubscribe, err := s.projectAssistantSupervisor().Subscribe(scope, runID, after)
	if errors.Is(err, store.ErrAssistantRunNotFound) {
		message, loadErr := s.findProjectMessage(r.Context(), scope, run.ActiveMessageID)
		if loadErr != nil {
			writeProjectError(w, loadErr)
			return
		}
		flusher, streamOK := startProjectAssistantSnapshotStream(w)
		if !streamOK {
			return
		}
		_ = writeProjectAssistantSnapshotSSE(w, flusher, projectAssistantRunSnapshot{Run: run, Message: message})
		return
	}
	if err != nil {
		writeProjectError(w, err)
		return
	}
	defer unsubscribe()
	flusher, streamOK := startProjectAssistantSnapshotStream(w)
	if !streamOK {
		return
	}
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case snapshot, open := <-updates:
			if !open {
				return
			}
			if err := writeProjectAssistantSnapshotSSE(w, flusher, snapshot); err != nil {
				return
			}
			if assistantRunTerminal(snapshot.Run.Status) {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func startProjectAssistantSnapshotStream(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStatus(w, http.StatusInternalServerError, "InternalError", "streaming unsupported")
		return nil, false
	}
	return flusher, true
}

func projectAssistantAfterRevision(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("afterRevision")
	}
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func writeProjectAssistantSnapshotSSE(w http.ResponseWriter, flusher http.Flusher, snapshot projectAssistantRunSnapshot) error {
	data, err := json.Marshal(projectAssistantRunSnapshotToAPI(snapshot))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: snapshot\ndata: %s\n\n", snapshot.Run.Revision, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// reconcileOrphanedProjectAssistantWorkItems releases tasks whose owning run is
// no longer live.
//
// reconcileOrphanedProjectAssistantRun only ever inspects the LATEST run, which
// is sound for the run itself but leaves a hole for work items: once any newer
// run exists and reaches a terminal status, an older run that died mid-task is
// never examined again. Its work item stays `active` forever, and because an
// active item cannot be cancelled or listed for discard, it blocks every new
// edit plan in that project with nothing able to clear it — observed in
// production as a task wedged behind a run that had finished hours earlier.
//
// This walks the work items instead of the run log, so the repair does not
// depend on which run happens to be newest.
func (s *Server) projectAssistantWorkItemConflictMessage(ctx context.Context, scope store.Scope, actor string) string {
	const fallback = "this project already has an open assistant task, and a project runs one at a time. " +
		"Stop the current turn to suspend it, then either continue that task or discard it from \"Continue previous work\" in the composer."

	items, err := s.store.ListAssistantWorkItems(ctx, scope)
	if err != nil {
		return fallback
	}
	for _, item := range items {
		if item.CreatedBy != actor {
			continue
		}
		switch item.Status {
		case store.AssistantWorkItemStatusActive:
			if strings.TrimSpace(item.ActiveRunID) != "" {
				return "this project already has an assistant task in progress, and a project runs one at a time. " +
					"Press Stop to suspend it, then continue that task (it keeps its approved plan) or discard it from \"Continue previous work\" in the composer."
			}
			// Active with no run is the state reconciliation repairs; say so
			// rather than describing a Stop button there is nothing to press.
			return "this project has an assistant task whose run is no longer active. " +
				"Reload the project to release it, then continue that task or discard it from \"Continue previous work\" in the composer."
		case store.AssistantWorkItemStatusSuspended:
			return "this project has a suspended assistant task, and a project runs one at a time. " +
				"Choose it under \"Continue previous work\" in the composer to resume it, or discard it there to start something new."
		}
	}
	return fallback
}
