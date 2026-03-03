package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
)

// maxConsecutiveEmptyResults is the number of consecutive empty results
// from the same tool that triggers context cancellation. After this many
// empty results, the sub-agent is forcibly stopped because further calls
// will almost certainly return empty too (e.g. searching an empty vector
// store). Without this guard, sub-agents burn their entire LLM call
// budget rephrasing the same fruitless queries.
const maxConsecutiveEmptyResults = 3

// emptyResultTools lists tool names whose output is inspected for emptiness.
// Only tools known to return structured results with a "count" or "results"
// field are tracked.
var emptyResultTools = map[string]bool{
	"memory_search":    true,
	"graph_query":      true,
	"graph_get_entity": true,
}

// emptyResultsMiddleware cancels the sub-agent's context after
// maxConsecutiveEmptyResults consecutive empty results from the same tool.
// This prevents agents from burning their entire LLM budget searching
// an empty memory store. Concurrency-safe via mutex.
type emptyResultsMiddleware struct {
	mu       sync.Mutex
	counters map[string]int // toolName → consecutive empty count
}

// EmptyResultsMiddleware returns a Middleware that cancels the context
// after maxConsecutiveEmptyResults consecutive empty results from the
// same search tool (memory_search, graph_query, graph_get_entity).
// Without this middleware, a sub-agent searching an empty vector store
// would exhaust its entire LLM call budget rephrasing fruitless queries.
func EmptyResultsMiddleware() Middleware {
	return &emptyResultsMiddleware{
		counters: make(map[string]int),
	}
}

func (m *emptyResultsMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		// Only track known search tools.
		if !emptyResultTools[tc.ToolName] {
			return next(ctx, tc)
		}

		result, err := next(ctx, tc)
		if err != nil {
			return result, err
		}

		empty := isEmptyResult(result)

		m.mu.Lock()
		if empty {
			m.counters[tc.ToolName]++
		} else {
			delete(m.counters, tc.ToolName)
		}
		count := m.counters[tc.ToolName]
		m.mu.Unlock()

		if count >= maxConsecutiveEmptyResults {
			logr := logger.GetLogger(ctx).With(
				"fn", "EmptyResultsMiddleware",
				"tool", tc.ToolName,
				"consecutive_empty", count,
			)
			logr.Warn("consecutive empty results threshold reached, cancelling sub-agent context")

			// Cancel via CancelCauseFunc if available (set by create_agent
			// for sub-agents). The error message is informational — the
			// runner will see ctx.Err() and stop.
			cancelErr := fmt.Errorf(
				"empty results: tool %s returned empty results %d times consecutively. "+
					"The memory store likely has no relevant data. Stopping to avoid wasting budget",
				tc.ToolName, count)

			if cancel := cancelCauseFromContext(ctx); cancel != nil {
				cancel(cancelErr)
			}
		}

		return result, nil
	}
}

// isEmptyResult inspects a tool result to determine if it represents
// an empty/zero-result response. Supports typed responses (structs with
// Count/Results fields) and raw JSON.
func isEmptyResult(result any) bool {
	if result == nil {
		return true
	}

	// Try typed struct with Count field (e.g. MemorySearchResponse).
	type hasCount interface{ GetCount() int }
	if c, ok := result.(hasCount); ok {
		return c.GetCount() == 0
	}

	// Fall back to JSON introspection for marshaled responses.
	var raw []byte
	switch v := result.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		// Marshal the result to inspect it.
		var err error
		raw, err = json.Marshal(result)
		if err != nil {
			return false
		}
	}

	var parsed map[string]json.RawMessage
	if json.Unmarshal(raw, &parsed) != nil {
		return false
	}

	// Check "count" field.
	if countRaw, ok := parsed["count"]; ok {
		var count int
		if json.Unmarshal(countRaw, &count) == nil && count == 0 {
			return true
		}
	}

	// Check "results" field (empty array).
	if resultsRaw, ok := parsed["results"]; ok {
		var results []json.RawMessage
		if json.Unmarshal(resultsRaw, &results) == nil && len(results) == 0 {
			return true
		}
	}

	return false
}

// --- Context key for cancel cause ---

type cancelCauseKeyType struct{}

var cancelCauseKey = cancelCauseKeyType{}

// WithCancelCause stores a context.CancelCauseFunc in the context so that
// middleware (loop detection, empty results) can terminate the sub-agent
// run. Only sub-agents set this; the parent agent's context has no cancel
// function, preserving backward compatibility.
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
