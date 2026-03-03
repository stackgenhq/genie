package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/stackgenhq/genie/pkg/logger"
)

// maxConsecutiveRepeatCalls is the number of consecutive identical tool calls
// that triggers loop detection. Set to 2 so that the second identical call is
// blocked — a single successful execution should be enough; a duplicate
// indicates the model failed to recognise the tool's success response.
const maxConsecutiveRepeatCalls = 2

// maxConsecutiveEmptyResults is the number of consecutive empty results from
// a retrieval tool (even with different arguments) that triggers cancellation.
// This catches the case where the agent rephrases queries but the backing
// store has no relevant data.
const maxConsecutiveEmptyResults = 3

// maxConsecutiveToolFailures is the number of consecutive failures for the same
// tool that triggers a hard block.
const maxConsecutiveToolFailures = 3

// retrievalTools lists tool names that are retrieval-only. Used by:
//   - Loop detection: tracks consecutive empty results for these tools
//   - create_agent: decides if a sub-agent can be skipped when store is empty
//
// Keep this as the single source of truth for retrieval tool classification.
var retrievalTools = map[string]bool{
	"memory_search":       true,
	"graph_query":         true,
	"graph_get_entity":    true,
	"graph_shortest_path": true,
}

// IsRetrievalTool reports whether the given tool name is classified as a
// retrieval-only tool (memory_search, graph_query, etc.).
func IsRetrievalTool(name string) bool {
	return retrievalTools[name]
}

// --- Loop Detection ---

// loopDetectionMiddleware detects two kinds of loops:
//  1. Argument loops: same tool + same args called consecutively
//     (maxConsecutiveRepeatCalls threshold, any tool)
//  2. Result loops: same retrieval tool returns empty results consecutively,
//     even with different args (maxConsecutiveEmptyResults threshold,
//     retrieval tools only)
//
// Each Wrapper gets its own instance so per-tool history is isolated.
// Concurrency-safe via mutex.
type loopDetectionMiddleware struct {
	mu           sync.Mutex
	history      []string       // bounded ring of "toolName:args" fingerprints
	emptyStreaks map[string]int // toolName → consecutive empty result count
}

// LoopDetectionMiddleware returns a Middleware that blocks a tool after
// maxConsecutiveRepeatCalls identical (same name + args) consecutive
// calls, and cancels the sub-agent after maxConsecutiveEmptyResults
// consecutive empty results from retrieval tools. This prevents infinite
// agent loops where the LLM keeps issuing the same call or rephrasing
// queries against an empty store.
func LoopDetectionMiddleware() Middleware {
	return &loopDetectionMiddleware{
		emptyStreaks: make(map[string]int),
	}
}

func (m *loopDetectionMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		argsStr := string(tc.Args)
		if cleaned, err := sjson.Delete(argsStr, "_justification"); err == nil {
			argsStr = cleaned
		}
		fingerprint := uuid.NewSHA1(uuid.Nil, []byte(tc.ToolName+":"+argsStr)).String()

		m.mu.Lock()
		looping := m.isLooping(fingerprint)
		if !looping {
			m.recordCall(fingerprint)
		}
		m.mu.Unlock()

		if looping {
			loopErr := fmt.Errorf(
				"loop detected: tool %s has been called with identical arguments %d times consecutively. "+
					"Stop calling this tool and summarize the results you already have",
				tc.ToolName, maxConsecutiveRepeatCalls)

			// Cancel the sub-agent's context so the runner stops
			// immediately. Without this, the LLM ignores the error
			// and keeps retrying, burning the entire call budget.
			// The cancel function is only set for sub-agents (via
			// WithCancelCause in create_agent.go); parent agents
			// get the error-only path for backward compatibility.
			if cancel := cancelCauseFromContext(ctx); cancel != nil {
				cancel(loopErr)
			}

			return nil, loopErr
		}

		result, err := next(ctx, tc)
		if err != nil {
			return result, err
		}

		// Track consecutive empty results for retrieval tools.
		// This catches agents that rephrase queries but always get empty
		// results because the backing store has no relevant data.
		if retrievalTools[tc.ToolName] {
			m.trackEmptyResult(ctx, tc.ToolName, result)
		}

		return result, nil
	}
}

// trackEmptyResult updates the consecutive-empty streak for a retrieval tool
// and cancels the sub-agent context when the threshold is reached.
func (m *loopDetectionMiddleware) trackEmptyResult(ctx context.Context, toolName string, result any) {
	empty := isEmptyResult(result)

	m.mu.Lock()
	if empty {
		m.emptyStreaks[toolName]++
	} else {
		delete(m.emptyStreaks, toolName)
	}
	count := m.emptyStreaks[toolName]
	m.mu.Unlock()

	if count == maxConsecutiveEmptyResults {
		logr := logger.GetLogger(ctx).With(
			"fn", "LoopDetectionMiddleware",
			"tool", toolName,
			"consecutive_empty", count,
		)
		logr.Warn("consecutive empty results threshold reached, cancelling sub-agent context")

		cancelErr := fmt.Errorf(
			"result loop: tool %s returned empty results %d times consecutively. "+
				"The backing store or data source likely has no relevant data. Stopping to avoid wasting budget",
			toolName, count)

		if cancel := cancelCauseFromContext(ctx); cancel != nil {
			cancel(cancelErr)
		}
	}
}

// isLooping returns true when the most recent entries match the fingerprint.
// Caller must hold m.mu.
func (m *loopDetectionMiddleware) isLooping(fingerprint string) bool {
	n := len(m.history)
	needed := maxConsecutiveRepeatCalls - 1
	if n < needed {
		return false
	}
	for i := n - needed; i < n; i++ {
		if m.history[i] != fingerprint {
			return false
		}
	}
	return true
}

// recordCall appends a fingerprint and trims to a bounded size.
// Caller must hold m.mu.
func (m *loopDetectionMiddleware) recordCall(fingerprint string) {
	m.history = append(m.history, fingerprint)
	const maxHistory = 10
	if len(m.history) > maxHistory {
		m.history = m.history[len(m.history)-maxHistory:]
	}
}

// isEmptyResult inspects a tool result to determine if it represents
// an empty/zero-result response. Uses gjson to check "count", "results",
// "found", and "path" fields in the JSON response.
func isEmptyResult(result any) bool {
	if result == nil {
		return true
	}

	var raw []byte
	switch v := result.(type) {
	case json.RawMessage:
		raw = v
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		var err error
		raw, err = json.Marshal(result)
		if err != nil {
			return false
		}
	}

	if !gjson.ValidBytes(raw) {
		return false
	}

	// Check "count" AND "results" fields. Some APIs use count=0 but still return results.
	c := gjson.GetBytes(raw, "count")
	r := gjson.GetBytes(raw, "results")
	if c.Exists() && c.Int() == 0 {
		// Only consider empty if results is also empty or missing
		if !r.Exists() || (r.IsArray() && len(r.Array()) == 0) {
			return true
		}
	} else if r.Exists() && r.IsArray() && len(r.Array()) == 0 {
		return true
	}

	// Check "found" field — false means empty (e.g. graph_get_entity).
	if f := gjson.GetBytes(raw, "found"); f.Exists() && !f.Bool() {
		return true
	}

	// Check "path" field — empty array means empty (e.g. graph_shortest_path).
	if p := gjson.GetBytes(raw, "path"); p.Exists() && p.IsArray() && len(p.Array()) == 0 {
		return true
	}

	return false
}

// --- Failure Limit ---

// failureLimitMiddleware blocks a tool after consecutive failures
// regardless of arguments. The counter resets on any success.
// Concurrency-safe via mutex.
type failureLimitMiddleware struct {
	mu       sync.Mutex
	failures map[string]int // toolName → consecutive failure count
}

// FailureLimitMiddleware returns a Middleware that blocks a tool after
// maxConsecutiveToolFailures consecutive errors (of any kind). Unlike
// CircuitBreakerMiddleware, this has no recovery timer — the tool stays
// blocked until a success. Use CircuitBreakerMiddleware for automatic
// recovery after a cooldown.
func FailureLimitMiddleware() Middleware {
	return &failureLimitMiddleware{
		failures: make(map[string]int),
	}
}

func (m *failureLimitMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		m.mu.Lock()
		count := m.failures[tc.ToolName]
		m.mu.Unlock()

		if count >= maxConsecutiveToolFailures {
			return nil, fmt.Errorf(
				"tool %s has failed %d times consecutively. The service may be rate-limited or down. "+
					"Stop calling this tool and report the failure to the user",
				tc.ToolName, count)
		}

		output, err := next(ctx, tc)

		m.mu.Lock()
		if err != nil {
			m.failures[tc.ToolName]++
		} else {
			delete(m.failures, tc.ToolName)
		}
		m.mu.Unlock()

		return output, err
	}
}

// --- Panic Recovery ---

// PanicRecoveryMiddleware returns a Middleware that recovers from panics
// in downstream handlers to prevent server crashes.
func PanicRecoveryMiddleware() Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (output any, err error) {
			logr := logger.GetLogger(ctx).With("fn", "PanicRecoveryMiddleware", "tool", tc.ToolName)
			defer func() {
				if r := recover(); r != nil {
					logr.Error("recovered panic in tool call", "panic", r, "tool", tc.ToolName)
					output = nil
					err = fmt.Errorf("internal error in tool %s: %v", tc.ToolName, r)
				}
			}()
			return next(ctx, tc)
		}
	})
}

// --- Context helpers ---

type cancelCauseKeyType struct{}

var cancelCauseKey = cancelCauseKeyType{}

// WithCancelCause stores a context.CancelCauseFunc in the context so that
// middleware (loop detection) can terminate the sub-agent run. Only sub-agents
// set this; the parent agent's context has no cancel function, preserving
// backward compatibility.
func WithCancelCause(ctx context.Context, fn context.CancelCauseFunc) context.Context {
	return context.WithValue(ctx, cancelCauseKey, fn)
}

// cancelCauseFromContext retrieves the CancelCauseFunc stored by
// WithCancelCause. Returns nil if no cancel function is present
// (e.g. parent agent context).
func cancelCauseFromContext(ctx context.Context) context.CancelCauseFunc {
	fn, _ := ctx.Value(cancelCauseKey).(context.CancelCauseFunc)
	return fn
}
