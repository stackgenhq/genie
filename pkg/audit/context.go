package audit

import "context"

// agentNameKey is the context key for the current agent name.
// Using a private type prevents collisions with other packages.
type agentNameKeyType struct{}

var agentNameKey = agentNameKeyType{}

// WithAgentName returns a copy of ctx carrying the given agent name.
// The DBAuditor extracts this value as a fallback when the LogRequest
// metadata does not contain an "agent" key.
//
// # Problem it solves
//
// # What happens without it
//
// without this, its hard to filter by agent in the audit log based on context

func WithAgentName(ctx context.Context, name string) context.Context {
	if existingAgent := AgentNameFromContext(ctx); existingAgent != "" {
		return ctx
	}
	return context.WithValue(ctx, agentNameKey, name)
}

// AgentNameFromContext extracts the agent name previously set via
// WithAgentName. Returns "" if not set.
func AgentNameFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(agentNameKey).(string); ok {
		return v
	}
	return ""
}
