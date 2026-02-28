package agui

import "context"

// Context keys for passing AG-UI session identifiers through the call chain.
type ctxKey int

const (
	ctxKeyThreadID ctxKey = iota
	ctxKeyRunID
)

// ThreadIDFromContext returns the ThreadID stored in the context by the AG-UI handler.
func ThreadIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyThreadID).(string); ok {
		return v
	}
	return ""
}

// RunIDFromContext returns the RunID stored in the context by the AG-UI handler.
func RunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRunID).(string); ok {
		return v
	}
	return ""
}

// WithThreadID stores the ThreadID in context so nested tools (e.g. create_agent)
// can propagate it to sub-agent tool wrappers for HITL correlation.
func WithThreadID(ctx context.Context, threadID string) context.Context {
	return context.WithValue(ctx, ctxKeyThreadID, threadID)
}

// WithRunID stores the RunID in context so nested tools (e.g. create_agent)
// can propagate it to sub-agent tool wrappers for HITL correlation.
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, ctxKeyRunID, runID)
}
