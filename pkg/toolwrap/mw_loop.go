// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/graph"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
)

// maxConsecutiveRepeatCalls is the number of consecutive identical tool calls
// (same name + same args) that triggers loop detection. Set to 2 so that the
// second identical call is blocked. Combined with the cancel-cause mechanism
// that terminates the sub-agent run, this prevents wasting budget on retries.
const maxConsecutiveRepeatCalls = 2

// maxConsecutiveSameToolCalls is the number of consecutive calls to the SAME
// tool (regardless of arguments) that triggers loop detection. This catches
// pagination/exploration loops where the LLM calls the same API with different
// parameters each time (e.g. scm_list_repos with Page=1, Page=2, Page=3...)
// instead of using the action tool directly.
const maxConsecutiveSameToolCalls = 4

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
	vector.MemorySearchToolName: true,
	graph.GraphQueryToolName:    true,
}

// IsRetrievalTool reports whether the given tool name is classified as a
// retrieval-only tool (memory_search, graph_query, etc.).
func IsRetrievalTool(name string) bool {
	return retrievalTools[name]
}

// defaultLoopExemptTools lists tool names that are exempt from loop detection
// by default. These are merged with any user-configured exempt tools.
// Exempt categories:
//   - note/read_notes: agent may call repeatedly to read/write parts of notes
//   - memory_*: idempotent read/write operations on the vector memory store.
//     Agents frequently re-search with the same query (e.g. checking for prior
//     knowledge on a clean slate), and blocking those calls disrupts normal
//     agent workflow. Includes memory_search, memory_store, memory_list,
//     memory_delete, and memory_merge.
//   - create_agent: each call spawns a distinct sub-agent with its own goal
//     and strategy. It is a delegation/orchestration tool, not a pagination
//     or discovery tool. Blocking it prevents the orchestrator from retrying
//     with different strategies after sub-agent failure.
var defaultLoopExemptTools = []string{
	"read_notes",
	"note",
	"memory_*",
	"create_agent",
}

// LoopDetectionConfig controls loop detection behaviour.
type LoopDetectionConfig struct {
	// ExemptTools lists additional tool names (or prefix patterns) to exempt
	// from loop detection. These are merged with the built-in defaults.
	// Supports exact names ("my_tool") and prefix patterns ("my_prefix_*").
	// Use this for custom tools that legitimately make many sequential calls
	// (e.g. read-only MCP tools).
	ExemptTools []string `yaml:"exempt_tools,omitempty" toml:"exempt_tools,omitempty"`
}

// loopExemptSet holds both exact tool names and prefix patterns for
// efficient matching.
type loopExemptSet struct {
	exact    map[string]bool
	prefixes []string
}

// buildExemptSet merges built-in defaults with user-configured exempt tools.
// Entries ending with "*" are treated as prefix patterns; all others are
// exact matches.
func (c LoopDetectionConfig) buildExemptSet() *loopExemptSet {
	all := make([]string, 0, len(defaultLoopExemptTools)+len(c.ExemptTools))
	all = append(all, defaultLoopExemptTools...)
	all = append(all, c.ExemptTools...)

	set := &loopExemptSet{exact: make(map[string]bool, len(all))}
	for _, entry := range all {
		if strings.HasSuffix(entry, "*") {
			set.prefixes = append(set.prefixes, strings.TrimSuffix(entry, "*"))
			continue
		}
		set.exact[entry] = true
	}
	return set
}

// isExempt returns true if the tool name matches an exact entry or any
// prefix pattern in the exempt set.
func (s *loopExemptSet) isExempt(toolName string) bool {
	if s.exact[toolName] {
		return true
	}
	for _, p := range s.prefixes {
		if strings.HasPrefix(toolName, p) {
			return true
		}
	}
	return false
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
	mu              sync.Mutex
	exemptTools     *loopExemptSet
	history         []string       // bounded ring of "toolName:args" fingerprints
	sameToolHistory []string       // bounded ring of tool names (no args)
	emptyStreaks    map[string]int // toolName → consecutive empty result count
}

// LoopDetectionMiddleware returns a Middleware that blocks a tool after
// maxConsecutiveRepeatCalls identical (same name + args) consecutive
// calls, and cancels the sub-agent after maxConsecutiveEmptyResults
// consecutive empty results from retrieval tools. This prevents infinite
// agent loops where the LLM keeps issuing the same call or rephrasing
// queries against an empty store.
//
// The config's ExemptTools are merged with built-in defaults so that
// user-configured exemptions extend (not replace) the safe list.
func LoopDetectionMiddleware(cfg ...LoopDetectionConfig) Middleware {
	var c LoopDetectionConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &loopDetectionMiddleware{
		exemptTools:  c.buildExemptSet(),
		emptyStreaks: make(map[string]int),
	}
}

func (m *loopDetectionMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		if m.exemptTools.isExempt(tc.ToolName) {
			result, err := next(ctx, tc)
			if err == nil && retrievalTools[tc.ToolName] {
				m.trackEmptyResult(ctx, tc.ToolName, result)
			}
			return result, err
		}

		// Internal tasks (e.g. graph learn) legitimately call the same tool
		// many times with different arguments. Skip same-tool loop detection
		// but keep identical-args loop detection to guard against true loops.
		// Retrieval tools (memory_search, graph_query) also skip same-tool
		// loop detection because they legitimately query with different terms
		// during exploration. Their dedicated empty-result tracking
		// (maxConsecutiveEmptyResults) provides the correct safety mechanism.
		isInternal := orchestratorcontext.IsInternalTask(ctx)

		argsStr := string(tc.Args)
		if cleaned, err := sjson.Delete(argsStr, "_justification"); err == nil {
			argsStr = cleaned
		}
		fingerprint := uuid.NewSHA1(uuid.Nil, []byte(tc.ToolName+":"+argsStr)).String()

		m.mu.Lock()
		identicalLoop := m.isLooping(fingerprint)
		sameToolLoop := !isInternal && !retrievalTools[tc.ToolName] && m.isSameToolLooping(tc.ToolName)
		if !identicalLoop && !sameToolLoop {
			m.recordCall(fingerprint)
			m.recordSameToolCall(tc.ToolName)
		}
		m.mu.Unlock()

		if identicalLoop {
			loopErr := fmt.Errorf(
				"loop detected: tool %s has been called with identical arguments %d times consecutively. "+
					"Stop calling this tool and summarize the results you already have",
				tc.ToolName, maxConsecutiveRepeatCalls)

			if cancel := cancelCauseFromContext(ctx); cancel != nil {
				cancel(loopErr)
			}
			return nil, loopErr
		}

		if sameToolLoop {
			loopErr := fmt.Errorf(
				"exploration loop detected: tool %s has been called %d times consecutively with different arguments. "+
					"This looks like unnecessary pagination or discovery. Use the information you already have "+
					"or call a different tool to complete the task",
				tc.ToolName, maxConsecutiveSameToolCalls)

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

// isSameToolLooping returns true when the most recent entries in the
// same-tool history all match the given tool name, indicating the LLM
// is calling the same tool repeatedly with different arguments (e.g.
// pagination loops). Caller must hold m.mu.
func (m *loopDetectionMiddleware) isSameToolLooping(toolName string) bool {
	n := len(m.sameToolHistory)
	needed := maxConsecutiveSameToolCalls - 1
	if n < needed {
		return false
	}
	for i := n - needed; i < n; i++ {
		if m.sameToolHistory[i] != toolName {
			return false
		}
	}
	return true
}

// recordSameToolCall appends a tool name to the same-tool history.
// Caller must hold m.mu.
func (m *loopDetectionMiddleware) recordSameToolCall(toolName string) {
	m.sameToolHistory = append(m.sameToolHistory, toolName)
	const maxHistory = 10
	if len(m.sameToolHistory) > maxHistory {
		m.sameToolHistory = m.sameToolHistory[len(m.sameToolHistory)-maxHistory:]
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

	// Check "found" field — false means empty (e.g. graph_query action=get_entity).
	if f := gjson.GetBytes(raw, "found"); f.Exists() && !f.Bool() {
		return true
	}

	// Check "path" field — empty array means empty (e.g. graph_query action=shortest_path).
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
