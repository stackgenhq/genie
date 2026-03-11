// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
)

// ExpertFailureReflector uses a lightweight LLM call to generate verbal
// reflections on agent failures. It uses the efficiency model (cheapest/
// fastest) to keep costs negligible.
//
// This lives in the reactree package (rather than memory) to avoid
// import cycles: memory → expert → reactree → memory.
type ExpertFailureReflector struct {
	expert expert.Expert
}

// Ensure ExpertFailureReflector implements memory.FailureReflector
var _ memory.FailureReflector = (*ExpertFailureReflector)(nil)

// NewExpertFailureReflector creates a FailureReflector that uses the given
// expert to generate failure reflections. The expert should be configured
// with a cheap model (e.g., TaskEfficiency) to minimize cost.
func NewExpertFailureReflector(exp expert.Expert) *ExpertFailureReflector {
	return &ExpertFailureReflector{expert: exp}
}

// reflectionPromptTemplate is the prompt used to generate failure reflections.
// It asks for a concise, actionable summary suitable for injection into future prompts.
const reflectionPromptTemplate = `You are reviewing a failed AI agent task. Be concise and actionable.

Goal: %q
Error output: %q

In 2-3 sentences, explain:
1. What went wrong (root cause, not symptoms)
2. What the agent should try differently next time

Do NOT repeat the error text verbatim. Focus on actionable lessons.`

// Reflect generates a verbal reflection on the failure using a cheap LLM call.
// Returns empty string if the reflection fails (best-effort; never blocks the main flow).
func (r *ExpertFailureReflector) Reflect(ctx context.Context, req memory.FailureReflectionRequest) string {
	logr := logger.GetLogger(ctx).With("fn", "ExpertFailureReflector.Reflect")

	// Cap error output to prevent large errors from bloating the reflection prompt.
	errorOutput := req.ErrorOutput
	const maxErrorRunes = 500
	if runes := []rune(errorOutput); len(runes) > maxErrorRunes {
		errorOutput = string(runes[:maxErrorRunes]) + "... (truncated)"
	}

	prompt := fmt.Sprintf(reflectionPromptTemplate, req.Goal, errorOutput)

	resp, err := r.expert.Do(ctx, expert.Request{
		Message:  prompt,
		TaskType: modelprovider.TaskEfficiency,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
			Silent:            true,
		},
	})
	if err != nil {
		logr.Warn("failure reflection LLM call failed, skipping", "error", err)
		return ""
	}

	reflection := ""
	if len(resp.Choices) > 0 {
		reflection = resp.Choices[len(resp.Choices)-1].Message.Content
	}

	// Cap reflection length to prevent bloated future prompts.
	const maxReflectionRunes = 300
	runes := []rune(reflection)
	if len(runes) > maxReflectionRunes {
		reflection = string(runes[:maxReflectionRunes]) + "..."
	}

	logr.Info("failure reflection generated",
		"goal", req.Goal,
		"reflection_length", len(reflection),
	)

	return reflection
}

// generateFailureReflection is a helper that delegates to the FailureReflector
// if available, or returns a basic fallback reflection. This is called from
// NewAgentNodeFunc when an error-like output is detected.
func generateFailureReflection(ctx context.Context, goal, errorOutput string, reflector memory.FailureReflector) string {
	if reflector == nil {
		// No reflector available — return a basic fallback.
		const maxFallbackRunes = 200
		t := errorOutput
		if runes := []rune(errorOutput); len(runes) > maxFallbackRunes {
			t = string(runes[:maxFallbackRunes]) + "..."
		}
		return fmt.Sprintf("Task failed with output: %s", t)
	}

	return reflector.Reflect(ctx, memory.FailureReflectionRequest{
		Goal:        goal,
		ErrorOutput: errorOutput,
	})
}
