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
	"time"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/cron"
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

// WithApprovalCacheTTL sets the time-to-live for the shared HITL approval
// cache. After this duration a previously approved tool+args combination
// requires fresh human approval. Default is 10 minutes.
func WithApprovalCacheTTL(ttl time.Duration) ServiceOption {
	return func(s *Service) { s.cacheTTL = ttl }
}

// WithApproveList injects the in-memory approve list so the HITL middleware
// can auto-approve tools that the user added via "approve for X mins" in the UI.
func WithApproveList(list *ApproveList) ServiceOption {
	return func(s *Service) { s.approveList = list }
}

// WithExtraMiddleware appends caller-supplied middleware to the default
// chain. These run after the built-in middlewares (audit, cache, HITL,
// etc.) and just before the terminal tool-execution handler.
//
// Use this to inject domain-specific middleware (e.g. policy enforcement)
// from the host application without modifying the genie core.
func WithExtraMiddleware(mw ...Middleware) ServiceOption {
	return func(s *Service) { s.extraMiddlewares = append(s.extraMiddlewares, mw...) }
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
	}
	for _, o := range opts {
		o(s)
	}
	s.hitlCache = newApprovalCache(s.cacheTTL)
	cfg = s.config
	// Create a singleton circuit breaker shared across all Wrap() calls.
	// The underlying breaker map is shared, but per-agent scoping is
	// applied in Wrap() via WithScope() so that policy denials in one
	// agent only trip that agent's circuit, not a global circuit.
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
	auditor          audit.Auditor
	approvalStore    hitl.ApprovalStore
	summarize        SummarizeFunc
	config           MiddlewareConfig
	circuitBreaker   *CircuitBreakerMW // singleton, shared across all agents
	hitlCache        *approvalCache    // shared across all sub-agents so same tool+args isn't re-prompted
	approveList      *ApproveList      // optional in-memory temporary allowlist (shared with AG-UI server)
	cacheTTL         time.Duration     // TTL for hitlCache entries; 0 uses defaultCacheTTL
	extraMiddlewares []Middleware      // caller-injected middleware (e.g. policy enforcement)
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
	// AgentName, when set, scopes the circuit breaker to this agent.
	// Policy-denied failures in agent A will only open the circuit for
	// agent A, not for agent B using the same tool without the policy.
	// When empty, the global (unscoped) circuit breaker is used.
	AgentName string
}

// Wrap wraps each tool with the middleware chain. Each Wrapper is
// constructed with an eagerly-built middleware chain via DefaultMiddlewares.
func (s *Service) Wrap(tools []tool.Tool, req WrapRequest) []tool.Tool {
	wrapped := make([]tool.Tool, 0, len(tools))
	// --- HITL approval gate (only when an approval store is configured) ---
	otherMws := []Middleware{}
	if s.approvalStore != nil {
		opts := []HITLOption{
			WithSharedApprovalCache(s.hitlCache),
			WithHITLAuditor(s.auditor),
			WithApproveListOption(s.approveList),
		}
		otherMws = append(otherMws, HITLApprovalMiddleware(
			s.approvalStore,
			req.WorkingMemory,
			opts...,
		))
	}

	// --- Extra middleware injected by the host application ---
	// Appended after HITL so policy enforcement (or any caller-supplied
	// middleware) sits closest to the terminal handler.
	otherMws = append(otherMws, s.extraMiddlewares...)

	// Scope the circuit breaker per-agent when an AgentName is provided.
	// This ensures policy denials (which are agent-specific) only trip
	// the circuit for that agent, not globally.
	cb := s.circuitBreaker
	if cb != nil && req.AgentName != "" {
		cb = cb.WithScope(req.AgentName)
	}

	for _, t := range tools {
		deps := MiddlewareDeps{
			WorkingMemory:  req.WorkingMemory,
			Auditor:        s.auditor,
			Summarize:      s.summarize,
			Config:         s.config,
			CircuitBreaker: cb,
			SemanticKeyFields: map[string][]string{
				cron.ToolName: {"name"},
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
