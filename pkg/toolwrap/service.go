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
	"context"

	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/messenger"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func NewService(
	auditor audit.Auditor,
	approvalStore hitl.ApprovalStore,
	fn SummarizeFunc,
) *Service {
	cfg := DefaultMiddlewareConfig()
	s := &Service{
		auditor:       auditor,
		approvalStore: approvalStore,
		config:        cfg,
	}
	// Create a singleton circuit breaker shared across all Wrap() calls.
	// When a tool fails in one sub-agent, the circuit opens for ALL agents
	// — no agent needs to independently discover the outage.
	if cfg.CircuitBreaker.Enabled {
		s.circuitBreaker = CircuitBreakerMiddleware(cfg.CircuitBreaker)
	}
	if fn == nil {
		fn = func(ctx context.Context, content string) (string, error) {
			return content, nil
		}
	}
	s.summarize = fn
	return s
}

// Service holds the session-stable dependencies for tool wrapping.
// Create one at startup and reuse it for every request.
type Service struct {
	auditor        audit.Auditor
	approvalStore  hitl.ApprovalStore
	summarize      SummarizeFunc
	config         MiddlewareConfig
	circuitBreaker *CircuitBreakerMW // singleton, shared across all agents
}

// CircuitBreaker returns the shared circuit breaker instance, or nil if
// circuit breaking is disabled. Callers can use OpenTools() to query
// which tools are currently tripped.
func (s *Service) CircuitBreaker() *CircuitBreakerMW {
	return s.circuitBreaker
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
	MessageOrigin messenger.MessageOrigin
}

// Wrap wraps each tool with the middleware chain. Each Wrapper is
// constructed with an eagerly-built middleware chain via DefaultMiddlewares.
func (s *Service) Wrap(tools []tool.Tool, req WrapRequest) []tool.Tool {
	wrapped := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		deps := MiddlewareDeps{
			EventChan:      req.EventChan,
			WorkingMemory:  req.WorkingMemory,
			ThreadID:       req.ThreadID,
			RunID:          req.RunID,
			MessageOrigin:  req.MessageOrigin,
			ApprovalStore:  s.approvalStore,
			Auditor:        s.auditor,
			Summarize:      s.summarize,
			Config:         s.config,
			CircuitBreaker: s.circuitBreaker,
			SemanticKeyFields: map[string][]string{
				"create_recurring_task": {"name"},
				// Cache http_request by URL+method so the same page
				// isn't fetched multiple times within a run (TTL=120s).
				// The trace showed openai.com/news/ fetched 3× for the
				// same useless result each time.
				"http_request": {"url", "method"},
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
