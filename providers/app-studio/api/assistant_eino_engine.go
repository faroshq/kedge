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
	"errors"

	"github.com/cloudwego/eino/adk"
)

type projectEinoAssistantEngine struct {
	current projectAssistantEngine
	runner  *adk.Runner
}

// NewEinoAssistantEngine returns the opt-in Eino shadow engine. The engine
// proves the Eino ADK runtime can be constructed in production code while the
// current App Studio chat loop remains the behavior source until the default
// engine flips later in the stack.
func NewEinoAssistantEngine(server *Server) projectAssistantEngine {
	return projectEinoAssistantEngine{
		current: projectChatCompletionAssistantEngine{server: server},
		runner:  adk.NewRunner(context.Background(), adk.RunnerConfig{EnableStreaming: true}),
	}
}

func (e projectEinoAssistantEngine) StreamProjectAssistant(
	ctx context.Context,
	req projectAssistantRunRequest,
	sink projectAssistantEventSink,
) (projectAssistantRunResult, error) {
	if req.Project == nil {
		return projectAssistantRunResult{}, errors.New("project is required")
	}
	if e.runner == nil {
		return projectAssistantRunResult{}, errors.New("eino runner is not configured")
	}
	if e.current == nil {
		return projectAssistantRunResult{}, errors.New("current assistant engine is not configured")
	}
	return e.current.StreamProjectAssistant(ctx, req, sink)
}
