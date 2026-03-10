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
)

// ActionReflector performs a Reasoning-Action-Reflection (RAR) loop before external actions.
// When enabled, the agent is prompted to justify why it is about to take an action,
// producing an "internal monologue" that is recorded for auditability and prevents
// hallucinated side-effectful calls.
//
//counterfeiter:generate . ActionReflector
type ActionReflector interface {
	// Reflect takes a proposed goal and the output from the most recent agent
	// node execution, and produces a reflection text. If the reflection
	// determines the action is unsafe or illogical, it returns an error.
	Reflect(ctx context.Context, req ReflectionRequest) (ReflectionResult, error)
}

// ReflectionRequest contains the inputs for a reflection step.
type ReflectionRequest struct {
	// Goal is the current task goal.
	Goal string
	// ProposedOutput is the output the agent is about to produce.
	ProposedOutput string
	// ToolCallsMade lists the tool names that were invoked in this turn.
	ToolCallsMade []string
	// IterationCount is the current iteration number.
	IterationCount int
}

// ReflectionResult captures the outcome of a reflection step.
type ReflectionResult struct {
	// Monologue is the internal justification produced by the reflector.
	Monologue string
	// ShouldProceed indicates whether the action should continue.
	ShouldProceed bool
}

// ExpertReflector uses a lightweight LLM call to produce reflections.
// It uses the front-desk/efficiency model to keep costs low.
type ExpertReflector struct {
	expert expert.Expert
}

// Ensure ExpertReflector implements ActionReflector
var _ ActionReflector = (*ExpertReflector)(nil)

// NewExpertReflector creates a reflector that uses the given expert for reflection prompts.
// The expert should be configured with a cheap/fast model (e.g. TaskEfficiency).
func NewExpertReflector(exp expert.Expert) *ExpertReflector {
	return &ExpertReflector{expert: exp}
}

// Reflect generates an internal monologue evaluating whether the proposed action
// is aligned with the goal. This is the "R" in the RAR loop.
func (r *ExpertReflector) Reflect(ctx context.Context, req ReflectionRequest) (ReflectionResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "ExpertReflector.Reflect")

	prompt := fmt.Sprintf(
		"You are a safety reviewer for an AI agent. "+
			"The agent's goal is: %q\n\n"+
			"The agent just completed iteration %d and produced:\n%s\n\n"+
			"Tools called: %v\n\n"+
			"Answer these questions in 2-3 sentences:\n"+
			"1. Is this output aligned with the goal?\n"+
			"2. Were the tool calls necessary and safe?\n"+
			"3. Should the agent proceed with the next iteration?\n"+
			"End with either PROCEED or HALT.",
		req.Goal, req.IterationCount, req.ProposedOutput, req.ToolCallsMade,
	)

	resp, err := r.expert.Do(ctx, expert.Request{
		Message:  prompt,
		TaskType: modelprovider.TaskEfficiency,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
		},
	})
	if err != nil {
		logr.Warn("reflection call failed, proceeding by default", "error", err)
		return ReflectionResult{
			Monologue:     fmt.Sprintf("Reflection failed: %v", err),
			ShouldProceed: true, // Fail-open: don't block on reflection errors
		}, nil
	}

	monologue := ""
	if len(resp.Choices) > 0 {
		monologue = resp.Choices[len(resp.Choices)-1].Message.Content
	}

	shouldProceed := true
	if len(monologue) > 0 {
		// Check the last 20 chars for HALT directive
		tail := monologue
		if len(tail) > 20 {
			tail = tail[len(tail)-20:]
		}
		if contains(tail, "HALT") {
			shouldProceed = false
		}
	}

	logr.Info("reflection completed",
		"should_proceed", shouldProceed,
		"monologue_length", len(monologue),
	)

	return ReflectionResult{
		Monologue:     monologue,
		ShouldProceed: shouldProceed,
	}, nil
}

// NoOpReflector is a no-op implementation that always proceeds.
// Used when action reflection is disabled.
type NoOpReflector struct{}

// Ensure NoOpReflector implements ActionReflector
var _ ActionReflector = (*NoOpReflector)(nil)

// Reflect always returns a proceed result with no monologue.
func (n *NoOpReflector) Reflect(_ context.Context, _ ReflectionRequest) (ReflectionResult, error) {
	return ReflectionResult{ShouldProceed: true}, nil
}

// contains checks if s contains substr (case-insensitive helper).
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c := s[i+j]
			sc := substr[j]
			// Simple uppercase check for ASCII
			if c >= 'a' && c <= 'z' {
				c -= 32
			}
			if sc >= 'a' && sc <= 'z' {
				sc -= 32
			}
			if c != sc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
