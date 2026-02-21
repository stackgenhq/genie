package toolwrap

import (
	"context"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
)

// contextEnrichMiddleware injects EventChan, ThreadID, RunID, and
// MessageOrigin into the context before the tool executes.
type contextEnrichMiddleware struct {
	eventChan chan<- interface{}
	threadID  string
	runID     string
	origin    *messenger.MessageOrigin
}

// ContextEnrichMiddleware creates a new context enrichment middleware.
func ContextEnrichMiddleware(
	eventChan chan<- interface{},
	threadID, runID string,
	origin *messenger.MessageOrigin,
) Middleware {
	return &contextEnrichMiddleware{
		eventChan: eventChan,
		threadID:  threadID,
		runID:     runID,
		origin:    origin,
	}
}

func (m *contextEnrichMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		logr := logger.GetLogger(ctx).With("fn", "ContextEnrichMiddleware", "tool", tc.ToolName)

		// Re-inject MessageOrigin if the context lost it.
		if m.origin != nil && messenger.MessageOriginFrom(ctx) == nil {
			ctx = messenger.WithMessageOrigin(ctx, m.origin)
			logr.Debug("re-injected MessageOrigin")
		}

		// Enrich context with EventChan/ThreadID/RunID.
		if m.eventChan != nil && agui.EventChanFromContext(ctx) == nil {
			ctx = agui.WithEventChan(ctx, m.eventChan)
		}
		if tid := m.effectiveThreadID(ctx); tid != "" && agui.ThreadIDFromContext(ctx) == "" {
			ctx = agui.WithThreadID(ctx, tid)
		}
		if rid := m.effectiveRunID(ctx); rid != "" && agui.RunIDFromContext(ctx) == "" {
			ctx = agui.WithRunID(ctx, rid)
		}

		return next(ctx, tc)
	}
}

func (m *contextEnrichMiddleware) effectiveThreadID(ctx context.Context) string {
	if m.threadID != "" {
		return m.threadID
	}
	return agui.ThreadIDFromContext(ctx)
}

func (m *contextEnrichMiddleware) effectiveRunID(ctx context.Context) string {
	if m.runID != "" {
		return m.runID
	}
	return agui.RunIDFromContext(ctx)
}
