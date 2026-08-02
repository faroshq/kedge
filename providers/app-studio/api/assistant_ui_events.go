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

const projectAssistantUIDevelopmentPreviewRefreshKey = "development.previewRefreshNeeded"

type projectAssistantUIEvent struct {
	DataModelUpdate  *projectAssistantUIDataModelUpdate  `json:"dataModelUpdate,omitempty"`
	InterruptRequest *projectAssistantUIInterruptRequest `json:"interruptRequest,omitempty"`
}

type projectAssistantUIDataModelUpdate struct {
	SurfaceID string                          `json:"surfaceId"`
	Contents  []projectAssistantUIDataContent `json:"contents,omitempty"`
}

type projectAssistantUIDataContent struct {
	Key         string `json:"key"`
	ValueString string `json:"valueString,omitempty"`
	Append      bool   `json:"append,omitempty"`
}

type projectAssistantUIInterruptRequest struct {
	InterruptID string                             `json:"interruptId"`
	Kind        string                             `json:"kind,omitempty"`
	SurfaceID   string                             `json:"surfaceId,omitempty"`
	Description string                             `json:"description,omitempty"`
	Questions   []string                           `json:"questions,omitempty"`
	Status      string                             `json:"status,omitempty"`
	Action      *projectAssistantUIInterruptAction `json:"action,omitempty"`
}

type projectAssistantUIInterruptAction struct {
	RunID              string `json:"runId"`
	RequestID          string `json:"requestId"`
	AssistantMessageID string `json:"assistantMessageId,omitempty"`
}

func projectAssistantUIDevelopmentPreviewRefreshEvent() projectAssistantUIEvent {
	return projectAssistantUIEvent{
		DataModelUpdate: &projectAssistantUIDataModelUpdate{
			SurfaceID: "conversation",
			Contents: []projectAssistantUIDataContent{{
				Key:         projectAssistantUIDevelopmentPreviewRefreshKey,
				ValueString: "true",
			}},
		},
	}
}

func projectAssistantUIInterruptRequestEvent(surfaceID string, permission projectAssistantPermission, checkpoint projectAssistantCheckpoint) projectAssistantUIEvent {
	return projectAssistantUIEvent{
		InterruptRequest: projectAssistantUIInterruptRequestFromPermissionCheckpoint(surfaceID, permission, checkpoint),
	}
}

func projectAssistantUIFollowUpInterruptRequestEvent(surfaceID string, followUp projectAssistantFollowUp, checkpoint projectAssistantCheckpoint) projectAssistantUIEvent {
	return projectAssistantUIEvent{
		InterruptRequest: projectAssistantUIInterruptRequestFromFollowUpCheckpoint(surfaceID, followUp, checkpoint),
	}
}

func projectAssistantUIInterruptRequestFromPermissionCheckpoint(surfaceID string, permission projectAssistantPermission, checkpoint projectAssistantCheckpoint) *projectAssistantUIInterruptRequest {
	return &projectAssistantUIInterruptRequest{
		InterruptID: permission.ID,
		Kind:        "permission",
		SurfaceID:   surfaceID,
		Description: permission.Reason,
		Status:      "pending",
		Action: &projectAssistantUIInterruptAction{
			RunID:              checkpoint.ID,
			RequestID:          permission.ID,
			AssistantMessageID: surfaceID,
		},
	}
}

func projectAssistantUIInterruptRequestFromFollowUpCheckpoint(surfaceID string, followUp projectAssistantFollowUp, checkpoint projectAssistantCheckpoint) *projectAssistantUIInterruptRequest {
	return &projectAssistantUIInterruptRequest{
		InterruptID: followUp.ID,
		Kind:        "follow_up",
		SurfaceID:   surfaceID,
		Description: followUp.Prompt,
		Questions:   append([]string(nil), followUp.Questions...),
		Status:      "pending",
		Action: &projectAssistantUIInterruptAction{
			RunID:              checkpoint.ID,
			RequestID:          followUp.ID,
			AssistantMessageID: surfaceID,
		},
	}
}

func projectAssistantUIResolvedInterruptEvent(surfaceID, interruptID string) projectAssistantUIEvent {
	return projectAssistantUIEvent{
		InterruptRequest: &projectAssistantUIInterruptRequest{
			InterruptID: interruptID,
			SurfaceID:   surfaceID,
			Status:      "resolved",
		},
	}
}
