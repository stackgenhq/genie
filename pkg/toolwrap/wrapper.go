package toolwrap

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/pii"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const maxToolResultSize = 80000

// ToolCaller is the pluggable contract for invoking a tool.
//
// The default implementation casts to tool.CallableTool and calls Call()
// directly. Alternative implementations (e.g. toolexec) may route through
// Temporal activities, gRPC, or any other execution backend.
//
// This interface lives in toolwrap — not in toolexec — so the middleware
// package has zero knowledge of Temporal, keeping the dependency graph clean.
type ToolCaller interface {
	// CallTool executes the named tool with the given JSON arguments.
	// Implementations must be safe for concurrent use.
	CallTool(ctx context.Context, toolName string, args []byte) (any, error)
}

// DirectCaller is the default ToolCaller that invokes tools in-process
// via the tool.CallableTool interface. This is the legacy execution path
// and is used when no alternative caller is configured.
type DirectCaller struct{}

// CallTool implements ToolCaller by casting to tool.CallableTool. This is
// never used directly — it exists as documentation. The Wrapper.execute()
// method uses the same logic inline for backward compatibility.
func (DirectCaller) CallTool(_ context.Context, _ string, _ []byte) (any, error) {
	// Intentionally not implemented — the Wrapper has the concrete tool.Tool
	// reference and calls ct.Call() directly. This type exists so callers
	// can reference the default behaviour.
	return nil, fmt.Errorf("DirectCaller.CallTool must not be called directly; use Wrapper.execute")
}

// Wrapper wraps a tool with a composable middleware chain.
// After decoupling, the Wrapper is a thin shell: just the underlying tool
// and a pre-built middleware chain. All mutable state lives inside the
// middleware closures.
//
// When a ToolCaller is set, the terminal handler delegates to it instead
// of calling tool.CallableTool.Call() directly. This allows plugging in
// alternative execution strategies (e.g. Temporal activities) without
// changing any middleware.
type Wrapper struct {
	tool.Tool
	middleware Middleware
}

// NewWrapper creates a Wrapper with an eagerly-built middleware chain.
// This is the primary constructor for tests and callers that don't
// go through Service.Wrap.
func NewWrapper(t tool.Tool, deps MiddlewareDeps) *Wrapper {
	w := &Wrapper{Tool: t}
	w.middleware = deps.DefaultMiddlewares()
	return w
}

// MiddlewareDeps carries the dependencies needed by the default middleware
// chain. These are supplied per-request via WrapRequest and per-service
// via the Service constructor.
type MiddlewareDeps struct {
	Auditor audit.Auditor
	// SemanticKeyFields maps tool names to the JSON argument fields that
	// form the semantic identity of a call for deduplication.
	SemanticKeyFields map[string][]string
	// WorkingMemory is used for HITL feedback storage.
	WorkingMemory *rtmemory.WorkingMemory
	// Summarize is an optional function that condenses large tool results.
	// When non-nil, tool responses exceeding the threshold are automatically
	// summarized before being returned to the LLM. Typically backed by
	// agentutils.Summarizer.
	Summarize SummarizeFunc
	// SummarizeThreshold is the character count above which a tool result
	// triggers auto-summarization. When 0, defaultSummarizeThreshold is used.
	SummarizeThreshold int
	// Config holds opt-in middleware settings (metrics, tracing, retry, etc).
	// Zero value disables all optional middlewares.
	Config MiddlewareConfig
	// CircuitBreaker is an optional shared circuit breaker singleton.
	// When set, it is reused across all Wrap() calls so that tool
	// failures in one sub-agent trip the circuit for ALL agents.
	CircuitBreaker *CircuitBreakerMW
}

// Call executes the tool through the configured middleware chain.
func (w *Wrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	logr := logger.GetLogger(ctx).With("fn", "Wrapper.Call", "tool", w.Tool.Declaration().Name)

	logr.Debug("tool call started", "args", pii.Redact(string(jsonArgs)))
	defer func(t time.Time) {
		logr.Debug("tool call completed", "tool", w.Tool.Declaration().Name, "duration", time.Since(t).String())
	}(time.Now())

	tc := &ToolCallContext{
		ToolName:     w.Tool.Declaration().Name,
		OriginalArgs: jsonArgs,
		Args:         jsonArgs,
	}

	mw := w.middleware
	if mw == nil {
		mw = MiddlewareDeps{}.DefaultMiddlewares()
	}
	return Chain(w.execute, mw)(ctx, tc)
}

// execute is the terminal Handler that calls the real underlying tool.
//
// When a ToolCaller is configured on the Wrapper, the call is delegated
// to it (e.g. Temporal activity execution). If the caller returns a
// "not found" error (common for internal orchestrator tools like
// create_agent / finish_task that don't exist in the tool registry),
// execution falls back to the direct in-process path.
//
// Without this fallback, injecting a ToolCaller would break internal
// tools that are never registered in the external tool registry.
func (w *Wrapper) execute(ctx context.Context, tc *ToolCallContext) (any, error) {
	// Default: direct in-process invocation.
	ct, ok := w.Tool.(tool.CallableTool)
	if !ok {
		return nil, fmt.Errorf("tool is not callable")
	}
	return ct.Call(ctx, tc.Args)
}

// DefaultMiddlewares returns the standard middleware chain.
// The chain is ordered from outermost (runs first) to innermost (runs
// just before the terminal handler):
//
//	PanicRecovery → PIIRehydrate → [Tracing] → [Metrics] → Emitter → Logger → Audit →
//	[Timeout] → [RateLimit] → [CircuitBreaker] → [Concurrency] → [Retry] →
//	SemanticCache → LoopDetection → FailureLimit → [Validation] →
//	[Sanitize] → HITLApproval → ContextEnrich
//
// Bracketed items are opt-in and only included when their Enabled flag is
// true in MiddlewareConfig.
func (deps MiddlewareDeps) DefaultMiddlewares(
	others ...Middleware,
) Middleware {
	cfg := deps.Config
	mws := []Middleware{
		PanicRecoveryMiddleware(),
		PIIRehydrateMiddleware(), // Rehydrate [HIDDEN:hash] in args so tools receive real values.
	}

	// --- Observability (outermost so they capture everything) ---
	if cfg.Tracing.Enabled {
		mws = append(mws, TracingMiddleware())
	}
	if cfg.Metrics.Enabled {
		mws = append(mws, cfg.Metrics.MetricsMiddleware())
	}

	mws = append(mws,
		LoggerMiddleware(),
		AuditMiddleware(deps.Auditor),
	)

	// --- Resilience ---
	if cfg.Timeout.Enabled {
		mws = append(mws, PerToolTimeoutMiddleware(cfg.Timeout.PerTool, cfg.Timeout.Default))
	}
	if cfg.RateLimit.Enabled {
		mws = append(mws, RateLimitMiddleware(cfg.RateLimit))
	}
	if cfg.CircuitBreaker.Enabled {
		cb := deps.CircuitBreaker
		if cb == nil {
			cb = CircuitBreakerMiddleware(cfg.CircuitBreaker)
		}
		mws = append(mws, cb)
	}
	if cfg.Concurrency.Enabled {
		mws = append(mws, ConcurrencyMiddleware(cfg.Concurrency))
	}
	if cfg.Retry.Enabled {
		mws = append(mws, RetryMiddleware(cfg.Retry))
	}

	// --- Core logic ---
	// SemanticCache sits before LoopDetection so that a cache hit (which
	// does NOT re-execute the tool) is never counted toward the consecutive
	// repeat threshold.
	mws = append(mws,
		SemanticCacheMiddleware(deps.SemanticKeyFields),
		LoopDetectionMiddleware(),
		FailureLimitMiddleware(),
	)

	if !cfg.ContextModeConfig.Disabled {
		mws = append(mws, ContextModeMiddleware(cfg.ContextModeConfig))
	}

	if cfg.Sanitize.Enabled {
		mws = append(mws, OutputSanitizationMiddleware(
			cfg.Sanitize.PerTool, cfg.Sanitize.Replacement))
	}

	mws = append(mws,
		AutoSummarizeMiddleware(deps.Summarize, deps.SummarizeThreshold),
	)

	return CompositeMiddleware(append(mws, others...))
}

// --- Package-level helpers (no Wrapper dependency) ---

// truncateResponse caps a response string at maxToolResultSize, respecting
// multi-byte character boundaries.
func truncateResponse(s string) (string, bool) {
	if len(s) <= maxToolResultSize {
		return s, false
	}
	end := maxToolResultSize
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "\n... [truncated - full output saved to file]", true
}

// TruncateForAudit truncates a string to maxLen runes for audit log metadata.
func TruncateForAudit(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// sensitiveKeys lists JSON key substrings whose values are redacted in audit logs.
var sensitiveKeys = []string{
	"token", "password", "secret", "api_key",
	"credentials", "authorization",
}

const maxAuditArgBytes = 4096

// redactSensitiveArgs returns a sanitised version of the tool arguments for
// audit logging.
func redactSensitiveArgs(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	redacted := string(args)
	var redactPaths []string
	var walkJSON func(prefix string, result gjson.Result)
	walkJSON = func(prefix string, result gjson.Result) {
		if result.Type != gjson.JSON {
			return
		}
		result.ForEach(func(key, value gjson.Result) bool {
			fullPath := key.String()
			if prefix != "" {
				fullPath = prefix + "." + key.String()
			}
			k := strings.ToLower(key.String())
			for _, s := range sensitiveKeys {
				if strings.Contains(k, s) {
					redactPaths = append(redactPaths, fullPath)
					return true
				}
			}
			if value.Type == gjson.JSON {
				walkJSON(fullPath, value)
			}
			return true
		})
	}
	walkJSON("", gjson.Parse(redacted))

	for _, p := range redactPaths {
		if r, err := sjson.Set(redacted, p, "[REDACTED]"); err == nil {
			redacted = r
		}
	}
	if len(redacted) > maxAuditArgBytes {
		redacted = fmt.Sprintf(`{"_truncated":true,"_original_bytes":%d}`, len(redacted))
	}
	return redacted
}
