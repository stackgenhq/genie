// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory

import "context"

// FailureReflector generates verbal reflections on agent failures.
// Instead of storing raw error text (which is nearly useless for future
// context), it asks a cheap LLM to summarize what went wrong and what
// the agent should try differently next time.
//
// This is inspired by the Reflexion paper (Shinn et al., 2023) which
// showed that verbal reinforcement — storing reflections rather than
// raw trajectories — significantly improves agent self-correction.
//
// The interface is defined here (memory package) to avoid import cycles;
// the LLM-backed implementation lives in the reactree package.
//
//counterfeiter:generate . FailureReflector
type FailureReflector interface {
	// Reflect generates a 2-3 sentence reflection on why a task failed
	// and what the agent should try differently. Returns the reflection
	// text, or empty string if reflection is unavailable.
	Reflect(ctx context.Context, req FailureReflectionRequest) string
}

// FailureReflectionRequest holds the inputs for generating a failure reflection.
type FailureReflectionRequest struct {
	// Goal is the task the agent was trying to accomplish.
	Goal string
	// ErrorOutput is the error text or failed output from the agent.
	ErrorOutput string
}

// noOpFailureReflector returns empty reflections. Used when no expert is available.
type noOpFailureReflector struct{}

func (noOpFailureReflector) Reflect(_ context.Context, _ FailureReflectionRequest) string {
	return ""
}

// NewNoOpFailureReflector creates a FailureReflector that always returns empty.
func NewNoOpFailureReflector() FailureReflector {
	return noOpFailureReflector{}
}
