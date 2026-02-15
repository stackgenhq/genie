package agui

import "context"

// Context keys for passing AG-UI session identifiers through the call chain.
type ctxKey int

const (
	ctxKeyThreadID ctxKey = iota
	ctxKeyRunID
	ctxKeyEventChan
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

// WithEventChan stores the event channel in context so sub-agent tool wrappers
// can emit HITL approval events back to the parent UI stream.
func WithEventChan(ctx context.Context, ch chan<- interface{}) context.Context {
	return context.WithValue(ctx, ctxKeyEventChan, ch)
}

// EventChanFromContext returns the EventChan stored in context, or nil.
func EventChanFromContext(ctx context.Context) chan<- interface{} {
	if v, ok := ctx.Value(ctxKeyEventChan).(chan<- interface{}); ok {
		return v
	}
	return nil
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
