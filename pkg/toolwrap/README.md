# toolwrap — Composable Middleware for LLM Tool Execution

`toolwrap` is a library that applies composable middleware to LLM tool calls, following the same `func(next) next` pattern used by HTTP middleware stacks. Each middleware implements the `Middleware` interface and can be used independently, composed into chains, or mixed with custom implementations.

## Architecture

```
          ┌──────────────────────────────────────────────────────────────────┐
          │                        Wrapper.Call()                           │
          │                                                                  │
          │  ToolCallContext{ToolName, Args, OriginalArgs, Justification}   │
          └─────────────┬────────────────────────────────────────────────────┘
                        │
                        ▼
 ┌────────────────────────────────────────────────────────────────────────────┐
 │                        Middleware Chain                                    │
 │                                                                            │
 │  [ContextMode] → PanicRecovery → [Tracing] → [Metrics] → Emitter →        │
 │  Logger → Audit → [Timeout] → [RateLimit] → [CircuitBreaker] →            │
 │  [Concurrency] → [Retry] → LoopDetection → FailureLimit →                 │
 │  SemanticCache → [Validation] → [Sanitize] → HITLApproval →               │
 │  ContextEnrich → execute()                                                │
 │                                                                            │
 │  Always-on middlewares run unconditionally.                                │
 │  [Bracketed] middlewares are opt-in via MiddlewareConfig.                  │
 └────────────────────────────────────────────────────────────────────────────┘
```

## Core Types

```go
// Handler processes a single tool call.
type Handler func(ctx context.Context, tc *ToolCallContext) (any, error)

// Middleware wraps a Handler with cross-cutting behaviour.
type Middleware interface {
    Wrap(next Handler) Handler
}

// MiddlewareFunc is the function adapter for Middleware.
type MiddlewareFunc func(next Handler) Handler

// CompositeMiddleware composes a slice of Middlewares into one.
type CompositeMiddleware []Middleware
```

## Using as a Library

### Quick Start — Default Chain

```go
svc := toolwrap.NewService(auditor, approvalStore)
wrapped := svc.Wrap(tools, toolwrap.WrapRequest{
    EventChan: evChan,
    ThreadID:  threadID,
    RunID:     runID,
})
// wrapped tools now have all core middlewares applied
```

### Custom Chain — Pick and Choose

```go
mw := toolwrap.CompositeMiddleware{
    toolwrap.PanicRecoveryMiddleware(),
    toolwrap.TimeoutMiddleware(30 * time.Second),
    toolwrap.RateLimitMiddleware(toolwrap.RateLimitConfig{
        GlobalRatePerMinute: 60,
        PerToolRatePerMinute: map[string]float64{"web_search": 10},
    }),
    toolwrap.RetryMiddleware(toolwrap.RetryConfig{MaxAttempts: 3}),
    toolwrap.TracingMiddleware(),
}

w := &toolwrap.Wrapper{Tool: myTool}
w.SetMiddleware(mw) // or pass via NewWrapper + MiddlewareDeps
result, err := w.Call(ctx, args)
```

### Single Middleware — Standalone Use

Every middleware works independently. You can wrap any `Handler` directly:

```go
handler := toolwrap.TimeoutMiddleware(5 * time.Second).Wrap(myHandler)
result, err := handler(ctx, tc)
```

### Config-Driven — YAML/TOML

```yaml
toolwrap:
  context_mode:
    enabled: true     # opt-in; set to true to activate
    threshold: 20000    # compress responses above this char count
  timeout:
    enabled: true
    default: 30s
    per_tool:
      execute_code: 120s
      web_search: 15s
  rate_limit:
    enabled: true
    global_rate_per_minute: 60
    per_tool_rate_per_minute:
      web_search: 10
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    open_duration: 30s
  concurrency:
    enabled: true
    global_limit: 10
    per_tool_limits:
      web_search: 3
  retry:
    enabled: true
    max_attempts: 3
    initial_backoff: 500ms
    max_backoff: 10s
  metrics:
    enabled: true
    prefix: "myapp.tools"
  tracing:
    enabled: true
  sanitize:
    enabled: true
    replacement: "[REDACTED]"
    per_tool:
      read_file: ["API_KEY", "password", "secret"]
```

---

## Middleware Reference

### Always-On (Core)

These middlewares are always included in the default chain and have no config toggle.

---

#### PanicRecoveryMiddleware

**File:** `mw_loop.go` · **Position:** Outermost

Recovers from panics in any downstream handler (e.g. OTel `span.End` on a closed channel) and converts them into a structured error. Without this, a single panicking tool would crash the entire server process.

```go
toolwrap.PanicRecoveryMiddleware()
```

| Aspect | Detail |
|--------|--------|
| State | Stateless |
| Per-tool | N/A |
| Short-circuits | On panic → returns error |

---

#### LoopDetectionMiddleware

**File:** `mw_loop.go`

Detects when the LLM calls the same tool with identical arguments more than 3 times consecutively and blocks further calls. Uses SHA1-based fingerprinting with a bounded sliding window (10 entries). Prevents infinite loops where a stuck agent re-issues the same call.

```go
toolwrap.LoopDetectionMiddleware()
```

| Aspect | Detail |
|--------|--------|
| State | Per-instance mutex + `[]string` history |
| Per-tool | Implicit — fingerprint includes tool name |
| Threshold | 3 consecutive identical calls |
| Short-circuits | On loop → returns error |

---

#### FailureLimitMiddleware

**File:** `mw_loop.go`

Blocks a tool after 3 consecutive failures (any args), preventing the LLM from burning tokens on a service that's down or rate-limited. A single success resets the counter. Unlike CircuitBreaker, there is no automatic recovery timer — the tool stays blocked until success.

```go
toolwrap.FailureLimitMiddleware()
```

| Aspect | Detail |
|--------|--------|
| State | `map[string]int` (tool → failure count) |
| Per-tool | ✅ independent counter per tool name |
| Threshold | 3 consecutive failures |
| Recovery | On any success for that tool |

---

#### SemanticCacheMiddleware

**File:** `mw_cache.go`

Deduplicates idempotent tool calls by caching results keyed on configurable semantic identity fields. Only tools with configured key fields are eligible. Entries expire after 120 seconds (TTL). Max 128 entries.

```go
toolwrap.SemanticCacheMiddleware(map[string][]string{
    "create_recurring_task": {"name"},
})
```

| Aspect | Detail |
|--------|--------|
| State | `TTLMap[any]` (bounded, TTL-based) |
| Per-tool | ✅ key fields configured per tool |
| TTL | 120s |
| Short-circuits | On cache hit → returns cached result |

---

#### HITLApprovalMiddleware

**File:** `mw_hitl.go`

Gates non-readonly tool calls on human approval. Extracts the optional `_justification` field from args, creates an approval request via the `ApprovalStore`, emits a `TOOL_APPROVAL_REQUEST` event to the UI, and blocks until resolved. Maintains a session-scoped approval cache (256 entries, FIFO eviction) to avoid re-prompting for the same tool+args. Stores user feedback in WorkingMemory.

```go
toolwrap.HITLApprovalMiddleware(store, eventChan, threadID, runID, workingMemory)
```

| Aspect | Detail |
|--------|--------|
| State | Approval cache (`map[string]struct{}`) |
| Per-tool | ✅ `IsAllowed()` allowlists readonly tools |
| Approval cache | 256 entries, SHA-256 keyed |
| Short-circuits | On rejection → error; on feedback → re-planning error |

---

#### ContextEnrichMiddleware

**File:** `mw_context.go`

Injects `EventChan`, `ThreadID`, `RunID`, and `MessageOrigin` into the `context.Context` before tool execution. Ensures nested tools (e.g. sub-agents via `create_agent`) can propagate HITL values even when the runner creates a fresh Go context.

```go
toolwrap.ContextEnrichMiddleware(eventChan, threadID, runID, origin)
```

| Aspect | Detail |
|--------|--------|
| State | Stateless (reads from deps) |
| Per-tool | N/A — applies to all |
| Position | Innermost — runs just before `execute()` |

---

#### EmitterMiddleware

**File:** `mw_emitter.go`

Emits `AgentToolResponseMsg` events to the TUI event channel after every tool call, including the (truncated) response and any error. Falls back to `EventChanFromContext` for sub-agent calls.

```go
toolwrap.EmitterMiddleware(eventChan)
```

| Aspect | Detail |
|--------|--------|
| State | Stateless |
| Per-tool | N/A — emits for all tools |

---

#### LoggerMiddleware

**File:** `mw_logger.go`

Logs the outcome of every tool call — `Debug` level for success, `Error` level for failure — using the context logger.

```go
toolwrap.LoggerMiddleware()
```

---

#### AuditMiddleware

**File:** `mw_audit.go`

Writes tool call results to the audit trail via the `audit.Auditor` interface. Includes redacted args, response length, truncation status, and error. Falls back to a basic stderr JSON logger when no auditor is provided.

```go
toolwrap.AuditMiddleware(auditor)
```

---

### Opt-In Middlewares

These are enabled via `MiddlewareConfig` fields in the config file. All default to disabled.

---

#### TimeoutMiddleware / PerToolTimeoutMiddleware

**File:** `mw_timeout.go` · **Config:** `timeout`

Enforces a maximum execution time per tool call via `context.WithTimeout`. `PerToolTimeoutMiddleware` supports per-tool overrides with a fallback default.

```go
// Global timeout
toolwrap.TimeoutMiddleware(30 * time.Second)

// Per-tool overrides
toolwrap.PerToolTimeoutMiddleware(
    map[string]time.Duration{"execute_code": 2 * time.Minute},
    30 * time.Second, // fallback
)
```

**Config:**
```yaml
timeout:
  enabled: true
  default: 30s
  per_tool:
    execute_code: 120s
```

| Aspect | Detail |
|--------|--------|
| State | Stateless |
| Per-tool | ✅ via `per_tool` map |
| Short-circuits | On deadline exceeded → `context.DeadlineExceeded` |

---

#### RateLimitMiddleware

**File:** `mw_ratelimit.go` · **Config:** `rate_limit`  
**Backed by:** `golang.org/x/time/rate`

Token-bucket rate limiter with global and per-tool limits. When the bucket is exhausted, the call is rejected immediately (non-blocking). Per-tool limiters are lazily created on first use.

```go
toolwrap.RateLimitMiddleware(toolwrap.RateLimitConfig{
    GlobalRatePerMinute:  60,
    PerToolRatePerMinute: map[string]float64{"web_search": 10},
})
```

**Config:**
```yaml
rate_limit:
  enabled: true
  global_rate_per_minute: 60
  per_tool_rate_per_minute:
    web_search: 10
```

| Aspect | Detail |
|--------|--------|
| State | `*rate.Limiter` per tool (lazily created) |
| Per-tool | ✅ `per_tool_rate_per_minute` map |
| Algorithm | Token bucket (`golang.org/x/time/rate`) |
| Short-circuits | On limit exceeded → error |

---

#### CircuitBreakerMiddleware

**File:** `mw_circuitbreaker.go` · **Config:** `circuit_breaker`  
**Backed by:** `sony/gobreaker/v2`

Full three-state circuit breaker (closed → open → half-open → closed) with per-tool breaker instances. Each tool gets its own `TwoStepCircuitBreaker`. When the failure threshold is reached, the circuit opens and all calls fail fast. After the cooldown (`OpenDuration`), a probe call is allowed. If it succeeds, the circuit closes.

```go
toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
    FailureThreshold: 5,
    OpenDuration:     30 * time.Second,
    HalfOpenMaxCalls: 1,
})
```

**Config:**
```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5
  open_duration: 30s
  half_open_max_calls: 1
```

| Aspect | Detail |
|--------|--------|
| State | `*gobreaker.TwoStepCircuitBreaker` per tool |
| Per-tool | ✅ independent breaker per tool name |
| States | Closed → Open → Half-Open → Closed |
| Short-circuits | When open → error; when half-open and max probes reached → error |

---

#### ConcurrencyMiddleware

**File:** `mw_concurrency.go` · **Config:** `concurrency`  
**Backed by:** `golang.org/x/sync/semaphore`

Limits the number of concurrent tool executions using weighted semaphores. Supports both a global cap and per-tool overrides (bulkhead pattern). When the limit is reached, the call **blocks** until a slot frees up or the context is cancelled.

```go
toolwrap.ConcurrencyMiddleware(toolwrap.ConcurrencyConfig{
    GlobalLimit:   10,
    PerToolLimits: map[string]int{"web_search": 3},
})
```

**Config:**
```yaml
concurrency:
  enabled: true
  global_limit: 10
  per_tool_limits:
    web_search: 3
```

| Aspect | Detail |
|--------|--------|
| State | `*semaphore.Weighted` per tool + global |
| Per-tool | ✅ `per_tool_limits` map |
| Behaviour | Blocks (not rejects) until slot available |

---

#### RetryMiddleware

**File:** `mw_retry.go` · **Config:** `retry`  
**Backed by:** `cenkalti/backoff/v4`

Automatically retries failed tool calls with exponential backoff and jitter. The `Retryable` predicate decides whether an error is transient. Non-retryable errors propagate immediately via `backoff.Permanent`. Context cancellation stops retries.

```go
toolwrap.RetryMiddleware(toolwrap.RetryConfig{
    MaxAttempts:    3,
    InitialBackoff: 500 * time.Millisecond,
    MaxBackoff:     10 * time.Second,
    Retryable:      func(err error) bool { return !errors.Is(err, ErrFatal) },
})
```

**Config:**
```yaml
retry:
  enabled: true
  max_attempts: 3
  initial_backoff: 500ms
  max_backoff: 10s
```

| Aspect | Detail |
|--------|--------|
| State | Stateless per-call |
| Per-tool | N/A (global config) |
| Algorithm | Exponential backoff + jitter (`cenkalti/backoff/v4`) |
| Retryable | Configurable predicate (nil = all errors retryable) |

---

#### MetricsMiddleware

**File:** `mw_metrics.go` · **Config:** `metrics`

Records OpenTelemetry metrics for every tool call:

| Metric | Type | Description |
|--------|------|-------------|
| `{prefix}.call.count` | Counter | Total tool calls |
| `{prefix}.call.duration_ms` | Histogram | Call latency in ms |
| `{prefix}.call.errors` | Counter | Failed calls |

All metrics carry a `tool.name` attribute for per-tool breakdown.

```go
cfg.Metrics.MetricsMiddleware() // prefix from config
```

**Config:**
```yaml
metrics:
  enabled: true
  prefix: "myapp.tools"
```

---

#### TracingMiddleware

**File:** `mw_tracing.go` · **Config:** `tracing`

Creates an OpenTelemetry span for every tool call. Records tool name, argument size, errors, and injects span context for correlation with nested/sub-agent calls.

```go
toolwrap.TracingMiddleware()
```

**Config:**
```yaml
tracing:
  enabled: true
```

| Aspect | Detail |
|--------|--------|
| Span name | `tool.<tool_name>` |
| Attributes | `tool.name`, `tool.args.size` |
| Error handling | `span.RecordError()` + `codes.Error` |

---

#### InputValidationMiddleware

**File:** `mw_validate.go` · **Config:** `validation`

Validates tool call arguments against the tool's declared `InputSchema` before execution. Checks JSON validity and required field presence. Intentionally lightweight (no full JSON Schema validation) to keep the dependency footprint small.

```go
toolwrap.InputValidationMiddleware(func(name string) *tool.Declaration {
    return registry.Get(name)
})
```

| Aspect | Detail |
|--------|--------|
| Checks | Valid JSON + required fields from schema |
| Per-tool | ✅ uses each tool's declared schema |
| Short-circuits | On validation failure → error with field name |

---

#### OutputSanitizationMiddleware

**File:** `mw_sanitize.go` · **Config:** `sanitize`

Scrubs sensitive data from tool outputs using case-insensitive pattern matching. Patterns are configured **per tool** — tools not in the map pass through unmodified.

```go
toolwrap.OutputSanitizationMiddleware(
    map[string][]string{
        "read_file": {"API_KEY", "password", "secret"},
    },
    "[REDACTED]",
)
```

**Config:**
```yaml
sanitize:
  enabled: true
  replacement: "[REDACTED]"
  per_tool:
    read_file: ["API_KEY", "password", "secret"]
    execute_code: ["token"]
```

| Aspect | Detail |
|--------|--------|
| Matching | Case-insensitive substring |
| Per-tool | ✅ `per_tool` map (tool → patterns) |
| Position | Post-execution (sanitizes output before it reaches the LLM) |

---

#### ContextModeMiddleware

**File:** `mw_contextmode.go` · **Config:** `context_mode`

Compresses oversized tool responses using local BM25-like chunk scoring — no LLM call required. Chunks text on paragraph boundaries, scores each chunk against query terms extracted from the tool's input arguments, and returns only the top-K most relevant chunks in reading order. Sits before `AutoSummarizeMiddleware` as a cheap first-pass reducer: if context mode shrinks a response below the summarise threshold, the LLM summariser is never called.

```go
toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
    Threshold: 20000,
    MaxChunks: 10,
    ChunkSize: 800,
})
```

**Config:**
```yaml
context_mode:
  enabled: true       # opt-in; set true to activate
  threshold: 20000     # compress responses above this char count
  max_chunks: 10       # return at most this many top-scored chunks
  chunk_size: 800      # target chars per chunk
```

| Aspect | Detail |
|--------|--------|
| Algorithm | BM25-lite (TF-IDF with saturation + length normalisation) |
| State | Stateless |
| Per-tool | N/A — applies to all tools |
| Fallback | Boundary selection (first + last chunks) when no query terms available |
| Position | Post-execution, before `AutoSummarizeMiddleware` |

---

## Writing Custom Middleware

Implement the `Middleware` interface or use `MiddlewareFunc`:

```go
// As an interface
type myMiddleware struct{ /* deps */ }

func (m *myMiddleware) Wrap(next toolwrap.Handler) toolwrap.Handler {
    return func(ctx context.Context, tc *toolwrap.ToolCallContext) (any, error) {
        // before
        result, err := next(ctx, tc)
        // after
        return result, err
    }
}

// As a function
func MyMiddleware() toolwrap.MiddlewareFunc {
    return func(next toolwrap.Handler) toolwrap.Handler {
        return func(ctx context.Context, tc *toolwrap.ToolCallContext) (any, error) {
            // your logic
            return next(ctx, tc)
        }
    }
}
```

Compose into any chain:

```go
chain := toolwrap.CompositeMiddleware{
    MyMiddleware(),
    toolwrap.PanicRecoveryMiddleware(),
    toolwrap.TimeoutMiddleware(30 * time.Second),
}
```

## Files

| File | Contents |
|------|----------|
| `middleware.go` | Core types: `Handler`, `Middleware`, `MiddlewareFunc`, `CompositeMiddleware`, `Chain` |
| `config.go` | `MiddlewareConfig` — central config aggregating all sub-configs |
| `wrapper.go` | `Wrapper`, `NewWrapper`, `MiddlewareDeps`, `DefaultMiddlewares` |
| `service.go` | `Service`, `NewService`, `WrapRequest` — production entry point |
| `context.go` | `WithOriginalQuestion` / `OriginalQuestionFrom` context helpers |
| `mw_loop.go` | `PanicRecoveryMiddleware`, `LoopDetectionMiddleware`, `FailureLimitMiddleware` |
| `mw_cache.go` | `SemanticCacheMiddleware` |
| `mw_hitl.go` | `HITLApprovalMiddleware` |
| `mw_context.go` | `ContextEnrichMiddleware` |
| `mw_emitter.go` | `EmitterMiddleware` |
| `mw_logger.go` | `LoggerMiddleware` |
| `mw_audit.go` | `AuditMiddleware` |
| `mw_timeout.go` | `TimeoutMiddleware`, `PerToolTimeoutMiddleware` |
| `mw_ratelimit.go` | `RateLimitMiddleware` |
| `mw_circuitbreaker.go` | `CircuitBreakerMiddleware` |
| `mw_concurrency.go` | `ConcurrencyMiddleware` |
| `mw_retry.go` | `RetryMiddleware` |
| `mw_metrics.go` | `MetricsMiddleware` |
| `mw_tracing.go` | `TracingMiddleware` |
| `mw_validate.go` | `InputValidationMiddleware` |
| `mw_sanitize.go` | `OutputSanitizationMiddleware` |
| `mw_contextmode.go` | `ContextModeMiddleware` — local BM25 chunk compression |
| `mw_summarize.go` | `AutoSummarizeMiddleware` — LLM-backed summarisation |
