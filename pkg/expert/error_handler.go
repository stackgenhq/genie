// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package expert

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// HandleExpertError inspects errors returned from the expert runner.
// It converts internal errors (context cancellation, LLM limits) into
// user-friendly messages instead of leaking raw Go error strings.
func HandleExpertError(ctx context.Context, err error) (Response, error) {
	if err == nil {
		return Response{}, nil
	}

	// Log the actual error for debugging
	logger.GetLogger(ctx).Error("Expert error occurred", "error", err.Error(), "error_type", fmt.Sprintf("%T", err))

	errMsg := err.Error()

	// The runner returns a formatted error string when max tool iterations are exceeded.
	// See trpc-agent-go/internal/flow/processor/functioncall.go
	if strings.Contains(errMsg, "max tool iterations") {
		return Response{
			Choices: []model.Choice{
				{
					Message: model.NewAssistantMessage("I have run into my limits (max tool iterations). Do you want me to keep trying? (Reply 'yes' to continue)"),
				},
			},
		}, nil
	}

	// Max LLM calls exceeded — return a friendly message instead of the raw error.
	if strings.Contains(errMsg, "max LLM calls") {
		return Response{
			Choices: []model.Choice{
				{
					Message: model.NewAssistantMessage("I reached my processing limit for this request. Please try breaking it into smaller steps or be more specific about what you need."),
				},
			},
		}, nil
	}

	// Context cancellation / deadline — the request was interrupted or timed out.
	// Return a friendly message instead of leaking "context canceled" or
	// "persist event: context canceled" to the user.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(errMsg, "context canceled") || strings.Contains(errMsg, "context deadline exceeded") {
		return Response{
			Choices: []model.Choice{
				{
					Message: model.NewAssistantMessage("I was unable to complete this task — the request was interrupted or timed out. Please try again, or break the request into smaller steps."),
				},
			},
		}, nil
	}

	return Response{}, fmt.Errorf("failed to run the expert: %w", err)
}

// transientErrorPatterns are substrings found in upstream LLM provider errors
// that indicate a transient, retryable condition (e.g. 503, rate limits).
var transientErrorPatterns = []string{
	"503",
	"529",
	"overloaded",
	"high demand",
	"rate limit",
	"temporarily unavailable",
	"RESOURCE_EXHAUSTED",
	"capacity",
	"try again later",
}

// IsTransientError returns true if the error looks like a transient upstream
// LLM provider error (503 / rate-limit / overloaded) that is worth retrying.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, pattern := range transientErrorPatterns {
		if strings.Contains(msg, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
