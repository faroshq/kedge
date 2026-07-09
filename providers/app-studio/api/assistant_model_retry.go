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
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	// projectEinoModelRetryMaxAttempts is the total number of model-call
	// attempts (initial try plus retries) for transient provider failures.
	projectEinoModelRetryMaxAttempts = 3
	// projectEinoModelRetryBaseDelay is the first backoff interval; it doubles
	// each attempt (0.5s, 1s, 2s).
	projectEinoModelRetryBaseDelay = 500 * time.Millisecond
	// projectEinoModelRetryMaxDelay caps a single backoff interval.
	projectEinoModelRetryMaxDelay = 8 * time.Second
)

// projectEinoRetryingChatModel wraps a chat model with bounded exponential
// backoff for transient provider failures (rate limits, 5xx, overloaded,
// timeouts). A single failed request from the provider otherwise fails the
// whole assistant turn.
//
// Retries are only applied to the initial call. For Generate that is the entire
// request. For Stream only the setup error (before any chunk is delivered) is
// retried — once the reader is returned, chunks are consumed by the agent and a
// mid-stream retry would duplicate output, so those errors are surfaced as-is.
type projectEinoRetryingChatModel struct {
	inner       einomodel.BaseChatModel
	toolCalling einomodel.ToolCallingChatModel
	maxAttempts int
	baseDelay   time.Duration
}

// projectEinoWithRetries wraps model with retry behaviour. It preserves the
// ToolCallingChatModel capability when the underlying model provides it, which
// the ADK agent relies on for tool binding.
func projectEinoWithRetries(model einomodel.BaseChatModel) einomodel.BaseChatModel {
	if model == nil {
		return nil
	}
	if _, ok := model.(*projectEinoRetryingChatModel); ok {
		return model
	}
	wrapped := &projectEinoRetryingChatModel{
		inner:       model,
		maxAttempts: projectEinoModelRetryMaxAttempts,
		baseDelay:   projectEinoModelRetryBaseDelay,
	}
	if tc, ok := model.(einomodel.ToolCallingChatModel); ok {
		wrapped.toolCalling = tc
	}
	return wrapped
}

func (m *projectEinoRetryingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	var out *schema.Message
	err := projectEinoRetryDo(ctx, m.maxAttempts, m.baseDelay, func() error {
		msg, callErr := m.inner.Generate(ctx, input, opts...)
		if callErr != nil {
			return callErr
		}
		out = msg
		return nil
	})
	return out, err
}

func (m *projectEinoRetryingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	var reader *schema.StreamReader[*schema.Message]
	err := projectEinoRetryDo(ctx, m.maxAttempts, m.baseDelay, func() error {
		r, callErr := m.inner.Stream(ctx, input, opts...)
		if callErr != nil {
			return callErr
		}
		reader = r
		return nil
	})
	return reader, err
}

// WithTools mirrors the wrapped model's tool binding, returning a retrying
// wrapper around the tool-bound instance. It errors only if the underlying
// model does not support tool binding.
func (m *projectEinoRetryingChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	if m.toolCalling == nil {
		return nil, errors.New("underlying chat model does not support tool binding")
	}
	bound, err := m.toolCalling.WithTools(tools)
	if err != nil {
		return nil, err
	}
	wrapped := projectEinoWithRetries(bound)
	if tc, ok := wrapped.(einomodel.ToolCallingChatModel); ok {
		return tc, nil
	}
	return bound, nil
}

// projectEinoRetryDo runs fn with bounded exponential backoff on retryable
// errors, honouring context cancellation between attempts.
func projectEinoRetryDo(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = fn()
		if err == nil {
			return nil
		}
		if !projectEinoRetryableError(err) {
			return err
		}
		if attempt == maxAttempts-1 {
			break
		}
		delay := baseDelay << attempt
		if delay > projectEinoModelRetryMaxDelay || delay <= 0 {
			delay = projectEinoModelRetryMaxDelay
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

// projectEinoRetryableError reports whether an error from a model provider is a
// transient failure worth retrying. It matches on the common overload / rate
// limit / server-error / timeout signals across the OpenAI, Anthropic, and
// Gemini SDKs, which surface these as error strings rather than typed values.
func projectEinoRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"429",
		"500",
		"502",
		"503",
		"504",
		"529",
		"rate limit",
		"ratelimit",
		"overloaded",
		"too many requests",
		"timeout",
		"timed out",
		"deadline exceeded",
		"connection reset",
		"connection refused",
		"eof",
		"temporarily unavailable",
		"service unavailable",
		"internal server error",
		"bad gateway",
		"gateway timeout",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
