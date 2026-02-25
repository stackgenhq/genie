// Package toolwrap provides composable middleware for tool execution.
//
// Each Middleware wraps a Handler, returning a new Handler with added
// behaviour (audit logging, HITL approval, caching, loop detection, etc.).
// The Service assembles the default middleware chain via DefaultMiddlewares
// and callers can customise or extend the chain.
//
// The Service holds session-stable dependencies (Auditor, ApprovalStore)
// so callers only need to supply per-request fields (WorkingMemory,
// ThreadID, RunID) via WrapRequest.
package toolwrap

import (
	"context"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/hitl"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ServiceOption configures optional behaviour on a toolwrap Service.
type ServiceOption func(*Service)

// WithMiddlewareConfig overrides the default middleware configuration.
// Pass a per-agent config to enable rate limiting, tracing, retries, etc.
// on a per-agent basis. When omitted, DefaultMiddlewareConfig() is used.
func WithMiddlewareConfig(cfg MiddlewareConfig) ServiceOption {
	return func(s *Service) { s.config = cfg }
}

func NewService(
	auditor audit.Auditor,
	approvalStore hitl.ApprovalStore,
	fn SummarizeFunc,
	opts ...ServiceOption,
) *Service {
	cfg := DefaultMiddlewareConfig()
	s := &Service{
		auditor:       auditor,
		approvalStore: approvalStore,
		config:        cfg,
		hitlCache:     newApprovalCache(),
	}
	for _, o := range opts {
		o(s)
	}
	cfg = s.config
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
	hitlCache      *approvalCache    // shared across all sub-agents so same tool+args isn't re-prompted
}

// CircuitBreaker returns the shared circuit breaker instance, or nil if
// circuit breaking is disabled. Callers can use OpenTools() to query
// which tools are currently tripped.
func (s *Service) CircuitBreaker() *CircuitBreakerMW {
	return s.circuitBreaker
}

// WrapRequest contains the per-request fields needed when wrapping tools.
type WrapRequest struct {
	WorkingMemory *rtmemory.WorkingMemory
}

// Wrap wraps each tool with the middleware chain. Each Wrapper is
// constructed with an eagerly-built middleware chain via DefaultMiddlewares.
func (s *Service) Wrap(tools []tool.Tool, req WrapRequest) []tool.Tool {
	wrapped := make([]tool.Tool, 0, len(tools))
	// --- HITL approval gate (only when an approval store is configured) ---
	otherMws := []Middleware{}
	if s.approvalStore != nil {
		otherMws = append(otherMws, HITLApprovalMiddleware(
			s.approvalStore,
			req.WorkingMemory,
			WithSharedApprovalCache(s.hitlCache),
		))
	}

	for _, t := range tools {
		deps := MiddlewareDeps{
			WorkingMemory:  req.WorkingMemory,
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
			middleware: deps.DefaultMiddlewares(otherMws...),
		}
		wrapped = append(wrapped, w)
	}
	return wrapped
}
