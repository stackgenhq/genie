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
// Returns the zero-value Agent and false if none was set.
func AgentFromContext(ctx context.Context) Agent {
	a, ok := agentFromContext(ctx)
	if !ok {
		return Agent{
			Name: DefaultAgentName,
		}
	}
	return a
}
