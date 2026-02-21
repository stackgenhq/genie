// Package toolwrap provides composable middleware for tool execution.
//
// Each Middleware wraps a Handler, returning a new Handler with added
// behaviour (audit logging, HITL approval, caching, loop detection, etc.).
// The Service assembles the default middleware chain via DefaultMiddlewares
// and callers can customise or extend the chain.
//
// The Service holds session-stable dependencies (Auditor, ApprovalStore)
// so callers only need to supply per-request fields (EventChan, WorkingMemory,
// ThreadID, RunID) via WrapRequest.
package toolwrap

import (
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/messenger"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func NewService(
	auditor audit.Auditor,
	approvalStore hitl.ApprovalStore,
) *Service {
	return &Service{
		auditor:       auditor,
		approvalStore: approvalStore,
	}
}

// Service holds the session-stable dependencies for tool wrapping.
// Create one at startup and reuse it for every request.
type Service struct {
	auditor       audit.Auditor
	approvalStore hitl.ApprovalStore
}

// WrapRequest contains the per-request fields needed when wrapping tools.
type WrapRequest struct {
	EventChan     chan<- interface{}
	WorkingMemory *rtmemory.WorkingMemory
	ThreadID      string
	RunID         string
	// MessageOrigin carries structured origin info (platform, channel,
	// sender, thread). Propagated explicitly because sub-agent runners
	// may create a fresh Go context that loses parent context values.
	MessageOrigin *messenger.MessageOrigin
}

// Wrap wraps each tool with the middleware chain. Each Wrapper is
// constructed with an eagerly-built middleware chain via DefaultMiddlewares.
func (s *Service) Wrap(tools []tool.Tool, req WrapRequest) []tool.Tool {
	wrapped := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		deps := MiddlewareDeps{
			EventChan:     req.EventChan,
			WorkingMemory: req.WorkingMemory,
			ThreadID:      req.ThreadID,
			RunID:         req.RunID,
			MessageOrigin: req.MessageOrigin,
			ApprovalStore: s.approvalStore,
			Auditor:       s.auditor,
			SemanticKeyFields: map[string][]string{
				"create_recurring_task": {"name"},
			},
		}
		w := &Wrapper{
			Tool:       t,
			middleware: deps.DefaultMiddlewares(),
		}
		wrapped = append(wrapped, w)
	}
	return wrapped
}
