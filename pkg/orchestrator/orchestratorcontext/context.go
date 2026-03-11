// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

// Package orchestratorcontext provides context-scoped access to the
// running agent's identity.  By injecting an Agent into ctx early in
// the pipeline (e.g. at Bootstrap / Start), any downstream code can
// retrieve the agent identity without explicit function parameters.
//
// Usage:
//
//	ctx = orchestratorcontext.WithAgent(ctx, orchestratorcontext.Agent{Name: "my-bot"})
//	…
//	name := orchestratorcontext.AgentNameFromContext(ctx) // "my-bot"
package orchestratorcontext

import "context"

// DefaultAgentName is returned when no Agent has been set on the context.
const DefaultAgentName = "Genie"

type contextKey struct{}
type internalTaskKey struct{}

// WithInternalTask marks the context as carrying an internal task
// (cron trigger, heartbeat, webhook event). Downstream code uses
// IsInternalTask to bypass the semantic cache and classification
// pipeline, which would otherwise block or misclassify system events.
func WithInternalTask(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalTaskKey{}, true)
}

// IsInternalTask returns true if the context was marked with
// WithInternalTask. Used by the orchestrator to skip the semantic
// cache and classification for background system events.
func IsInternalTask(ctx context.Context) bool {
	v, _ := ctx.Value(internalTaskKey{}).(bool)
	return v
}

// Agent holds the identity of the currently running agent.
// For now it only carries a Name; additional fields (e.g. Version,
// Description) can be added later without breaking callers.
type Agent struct {
	Name string
}

// WithAgent returns a copy of ctx that carries the given Agent.
func WithAgent(ctx context.Context, a Agent) context.Context {
	agent, ok := agentFromContext(ctx)
	if ok && agent.Name != "" {
		return ctx
	}

	return context.WithValue(ctx, contextKey{}, a)
}

// agentFromContext extracts the Agent from ctx.
// Returns the zero-value Agent and false if none was set.
func agentFromContext(ctx context.Context) (Agent, bool) {
	a, ok := ctx.Value(contextKey{}).(Agent)
	return a, ok
}

// AgentFromContext extracts the Agent from ctx.
// If no Agent was set on the context, it returns an Agent whose Name is DefaultAgentName.
func AgentFromContext(ctx context.Context) Agent {
	a, ok := agentFromContext(ctx)
	if !ok {
		return Agent{
			Name: DefaultAgentName,
		}
	}
	return a
}

// AgentNameFromContext is a convenience function that returns the
// agent's Name from ctx, defaulting to DefaultAgentName.
func AgentNameFromContext(ctx context.Context) string {
	return AgentFromContext(ctx).Name
}
