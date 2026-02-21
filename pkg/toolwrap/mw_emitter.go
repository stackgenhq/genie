package toolwrap

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/agui"
)

// emitterMiddleware emits tool call results to the TUI event channel.
type emitterMiddleware struct {
	eventChan chan<- interface{}
}

// EmitterMiddleware creates a new emitter middleware.
func EmitterMiddleware(eventChan chan<- interface{}) Middleware {
	return &emitterMiddleware{eventChan: eventChan}
}

func (m *emitterMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		output, err := next(ctx, tc)

		responseStr, _ := truncateResponse(fmt.Sprintf("%v", output))

		// Resolve event channel: explicit field or context fallback.
		evChan := m.eventChan
		if evChan == nil {
			evChan = agui.EventChanFromContext(ctx)
		}
		if evChan != nil {
			evChan <- agui.AgentToolResponseMsg{
				Type:     agui.EventToolCallResult,
				ToolName: tc.ToolName,
				Response: responseStr,
				Error:    err,
			}
		}

		return output, err
	}
}
