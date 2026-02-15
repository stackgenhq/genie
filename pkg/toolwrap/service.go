// Package toolwrap provides a service that wraps tools with cross-cutting
// concerns: audit logging, file-read caching, HITL approval gating, and
// TUI event emission.
//
// The Service holds session-stable dependencies (Auditor, ApprovalStore)
// so callers only need to supply per-request fields (EventChan, WorkingMemory,
// ThreadID, RunID) via WrapRequest.
package toolwrap

import (
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/hitl"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Service holds the session-stable dependencies for tool wrapping.
// Create one at startup and reuse it for every request.
type Service struct {
	Auditor       audit.Auditor
	ApprovalStore hitl.ApprovalStore
}

// WrapRequest contains the per-request fields needed when wrapping tools.
type WrapRequest struct {
	EventChan     chan<- interface{}
	WorkingMemory *rtmemory.WorkingMemory
	ThreadID      string
	RunID         string
}

// Wrap wraps each tool with auditing, caching, HITL gating, and event emission.
// The returned slice preserves the original tool order.
func (s *Service) Wrap(tools []tool.Tool, req WrapRequest) []tool.Tool {
	wrapped := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		wrapped = append(wrapped, &Wrapper{
			Tool:          t,
			EventChan:     req.EventChan,
			WorkingMemory: req.WorkingMemory,
			Auditor:       s.Auditor,
			ApprovalStore: s.ApprovalStore,
			ThreadID:      req.ThreadID,
			RunID:         req.RunID,
		})
	}
	return wrapped
}
