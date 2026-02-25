package toolwrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/logger"
)

// maxConsecutiveRepeatCalls is the number of consecutive identical tool calls
// that triggers loop detection. Set to 2 so that the second identical call is
// blocked — a single successful execution should be enough; a duplicate
// indicates the model failed to recognise the tool's success response.
const maxConsecutiveRepeatCalls = 2

// maxConsecutiveToolFailures is the number of consecutive failures for the same
// tool that triggers a hard block.
const maxConsecutiveToolFailures = 3

// --- Loop Detection ---

// loopDetectionMiddleware detects consecutive identical tool calls and
// blocks the call when the threshold is reached. Each Wrapper gets its
// own instance so per-tool history is isolated. Concurrency-safe via mutex.
type loopDetectionMiddleware struct {
	mu      sync.Mutex
	history []string // bounded ring of "toolName:args" fingerprints
}

// LoopDetectionMiddleware returns a Middleware that blocks a tool after
// maxConsecutiveRepeatCalls identical (same name + args) consecutive
// calls. This prevents infinite agent loops where the LLM keeps issuing
// the same call. Without this middleware, a stuck agent could exhaust
// token budgets or rate limits by re-executing the same tool call.
func LoopDetectionMiddleware() Middleware {
	return &loopDetectionMiddleware{}
}

func (m *loopDetectionMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		fingerprint := uuid.NewSHA1(uuid.Nil, []byte(tc.ToolName+":"+string(tc.Args))).String()

		m.mu.Lock()
		looping := m.isLooping(fingerprint)
		if !looping {
			m.recordCall(fingerprint)
		}
		m.mu.Unlock()

		if looping {
			return nil, fmt.Errorf(
				"loop detected: tool %s has been called with identical arguments %d times consecutively. "+
					"Stop calling this tool and summarize the results you already have",
				tc.ToolName, maxConsecutiveRepeatCalls)
		}
		return next(ctx, tc)
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
