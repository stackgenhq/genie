// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package langfuse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
)

// --- Langfuse API types ---

// Trace represents a top-level trace entry from the Langfuse API.
// Each trace corresponds to one user request.
type Trace struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Name      string    `json:"name"`
	UserID    string    `json:"userId"`
	SessionID *string   `json:"sessionId"`
	Input     any       `json:"input"`
	Output    any       `json:"output"`
	Tags      []string  `json:"tags"`
	Version   string    `json:"version,omitempty"`
}

// Observation represents a single observation (span, generation, or event)
// within a trace. Observations form a tree via ParentObservationID.
type Observation struct {
	ID                  string     `json:"id"`
	TraceID             string     `json:"traceId"`
	ParentObservationID *string    `json:"parentObservationId"`
	Type                string     `json:"type"` // "GENERATION", "SPAN", "EVENT"
	Name                string     `json:"name"`
	StartTime           time.Time  `json:"startTime"`
	EndTime             *time.Time `json:"endTime,omitempty"`
	Model               string     `json:"model,omitempty"`
	Input               any        `json:"input,omitempty"`
	Output              any        `json:"output,omitempty"`
	Level               string     `json:"level,omitempty"`
	StatusMessage       string     `json:"statusMessage,omitempty"`
	Usage               *ObsUsage  `json:"usage,omitempty"`
}

// ObsUsage holds token usage for a generation observation.
type ObsUsage struct {
	Input  int     `json:"input"`
	Output int     `json:"output"`
	Total  int     `json:"total"`
	Unit   string  `json:"unit,omitempty"`
	Cost   float64 `json:"totalCost,omitempty"`
}

// tracesResponse is the Langfuse API response for GET /api/public/traces.
type tracesResponse struct {
	Data []Trace `json:"data"`
	Meta struct {
		TotalItems int `json:"totalItems"`
		Page       int `json:"page"`
	} `json:"meta"`
}

// observationsResponse is the Langfuse API response for GET /api/public/observations.
type observationsResponse struct {
	Data []Observation `json:"data"`
	Meta struct {
		TotalItems int `json:"totalItems"`
		Page       int `json:"page"`
	} `json:"meta"`
}

// --- Request/Result types ---

// AnalyzeTracesRequest holds the parameters for analyzing traces.
// All filter fields are optional — omit them to widen the query.
type AnalyzeTracesRequest struct {
	// UserID filters traces to a specific user. Optional.
	UserID string

	// SessionID filters traces to a specific session. Optional.
	SessionID string

	// AgentName filters traces by agent (trace name). Optional.
	AgentName string

	// Duration is the lookback window from now. Required when using Analyze().
	Duration time.Duration

	// Tags filters traces to those matching these tags. Optional.
	Tags []string

	// Limit caps the number of traces fetched. Defaults to 100.
	Limit int
}

// TraceAnalysisResult holds the aggregated analysis across all traces.
type TraceAnalysisResult struct {
	// TracesAnalyzed is the total number of traces processed.
	TracesAnalyzed int `json:"traces_analyzed"`

	// TraceDetails contains the per-trace execution breakdown.
	TraceDetails []TraceDetail `json:"trace_details"`

	// Aggregated totals across all traces.
	TotalToolCalls      int `json:"total_tool_calls"`
	TotalLLMCalls       int `json:"total_llm_calls"`
	TotalSubAgents      int `json:"total_sub_agents"`
	TotalVectorStoreOps int `json:"total_vector_store_ops"`
	TotalInputTokens    int `json:"total_input_tokens"`
	TotalOutputTokens   int `json:"total_output_tokens"`
}

// TraceDetail is the execution breakdown of a single trace (one user request).
type TraceDetail struct {
	// TraceID is the Langfuse trace ID.
	TraceID string `json:"trace_id"`

	// AgentName is the top-level agent (trace name).
	AgentName string `json:"agent_name"`

	// UserID is who made the request.
	UserID string `json:"user_id"`

	// SessionID is the conversation session.
	SessionID string `json:"session_id,omitempty"`

	// Timestamp is when the request was made.
	Timestamp time.Time `json:"timestamp"`

	// Input is the user's original request.
	Input string `json:"input"`

	// Output is the agent's final response (nil if no output).
	Output *string `json:"output"`

	// ToolCalls lists every tool invocation in this trace.
	ToolCalls []ToolCallDetail `json:"tool_calls"`

	// SubAgents lists every sub-agent that was created.
	SubAgents []SubAgentDetail `json:"sub_agents"`

	// LLMCalls is the total number of LLM generation calls.
	LLMCalls int `json:"llm_calls"`

	// VectorStoreOps is the number of vector store add operations.
	VectorStoreOps int `json:"vector_store_ops"`

	// InputTokens is the sum of input tokens across all generations.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the sum of output tokens across all generations.
	OutputTokens int `json:"output_tokens"`

	// TotalCost is the estimated total cost (USD).
	TotalCost float64 `json:"total_cost"`

	// Duration is the total trace duration (first observation to last).
	Duration time.Duration `json:"duration"`
}

// ToolCallDetail captures a single tool invocation.
type ToolCallDetail struct {
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	// ParentName is the name of the parent span/agent that made this call.
	ParentName string `json:"parent_name,omitempty"`
}

// SubAgentDetail captures a sub-agent execution.
type SubAgentDetail struct {
	Name      string `json:"name"`
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	LLMCalls  int    `json:"llm_calls"`
	ToolCalls int    `json:"tool_calls"`
}

// --- TraceAnalyzer ---

// TraceAnalyzer analyzes Langfuse traces to produce execution breakdowns.
// It can fetch traces from the Langfuse API or analyze local exports.
type TraceAnalyzer struct {
	httpClient *http.Client
	config     Config
}

// NewTraceAnalyzer creates a TraceAnalyzer that queries the Langfuse API.
// Returns nil if the config is missing credentials.
func (c Config) NewTraceAnalyzer() *TraceAnalyzer {
	if c.PublicKey == "" || c.SecretKey == "" || c.Host == "" {
		return nil
	}
	return &TraceAnalyzer{
		config: c,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Analyze fetches traces from the Langfuse API, then fetches observations
// for each trace, and produces an execution breakdown.
func (a *TraceAnalyzer) Analyze(ctx context.Context, req AnalyzeTracesRequest) (TraceAnalysisResult, error) {
	if a == nil {
		return TraceAnalysisResult{}, fmt.Errorf("trace analyzer is not configured")
	}

	logr := logger.GetLogger(ctx).With("fn", "TraceAnalyzer.Analyze")

	traces, err := a.fetchTraces(ctx, req)
	if err != nil {
		return TraceAnalysisResult{}, fmt.Errorf("failed to fetch traces: %w", err)
	}

	logr.Info("fetched traces for analysis",
		"count", len(traces),
		"user_id", req.UserID,
		"session_id", req.SessionID,
		"agent_name", req.AgentName,
	)

	var result TraceAnalysisResult

	for _, t := range traces {
		observations, fetchErr := a.fetchObservations(ctx, t.ID)
		if fetchErr != nil {
			logr.Warn("failed to fetch observations for trace, skipping",
				"trace_id", t.ID, "error", fetchErr)
			continue
		}
		detail := buildTraceDetail(t, observations)
		result.TraceDetails = append(result.TraceDetails, detail)
		result.TracesAnalyzed++
	}

	result.aggregate()

	return result, nil
}

// --- API calls ---

// fetchTraces queries GET /api/public/traces with filter parameters.
func (a *TraceAnalyzer) fetchTraces(ctx context.Context, req AnalyzeTracesRequest) ([]Trace, error) {
	if req.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %s", req.Duration)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	now := time.Now().UTC()
	from := now.Add(-req.Duration)

	params := url.Values{}
	params.Set("fromTimestamp", from.Format(time.RFC3339))
	params.Set("toTimestamp", now.Format(time.RFC3339))
	params.Set("limit", fmt.Sprintf("%d", limit))

	if req.UserID != "" {
		params.Set("userId", req.UserID)
	}
	if req.SessionID != "" {
		params.Set("sessionId", req.SessionID)
	}
	if req.AgentName != "" {
		params.Set("name", req.AgentName)
	}
	for _, tag := range req.Tags {
		params.Add("tags", tag)
	}

	apiURL := fmt.Sprintf("%s/api/public/traces?%s",
		a.config.langfuseHost(), params.Encode())

	body, err := a.fetchJSON(ctx, apiURL)
	if err != nil {
		return nil, err
	}

	var resp tracesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse traces response: %w", err)
	}

	return resp.Data, nil
}

// fetchObservations queries GET /api/public/observations?traceId=xxx.
// It paginates through all pages to collect the complete set of observations.
func (a *TraceAnalyzer) fetchObservations(ctx context.Context, traceID string) ([]Observation, error) {
	const pageSize = 500
	var all []Observation

	for page := 1; ; page++ {
		params := url.Values{}
		params.Set("traceId", traceID)
		params.Set("limit", fmt.Sprintf("%d", pageSize))
		params.Set("page", fmt.Sprintf("%d", page))

		apiURL := fmt.Sprintf("%s/api/public/observations?%s",
			a.config.langfuseHost(), params.Encode())

		body, err := a.fetchJSON(ctx, apiURL)
		if err != nil {
			return nil, err
		}

		var resp observationsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse observations response: %w", err)
		}

		all = append(all, resp.Data...)

		// Stop when this page was incomplete.
		if len(resp.Data) < pageSize {
			break
		}

		// If TotalItems is provided and positive, also stop once we've fetched all items.
		if resp.Meta.TotalItems > 0 && len(all) >= resp.Meta.TotalItems {
			break
		}
	}

	return all, nil
}

// fetchJSON performs an authed GET, reads the body, and returns it as raw bytes.
func (a *TraceAnalyzer) fetchJSON(ctx context.Context, apiURL string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.SetBasicAuth(a.config.PublicKey, a.config.SecretKey)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// --- Observation analysis ---

// Known tool-call span name patterns. Langfuse records tool calls as
// SPAN observations whose name is the tool function name.
var vectorStoreTools = map[string]bool{
	"addMemory":      true,
	"add_memory":     true,
	"store_memory":   true,
	"AddMemory":      true,
	"vector_store":   true,
	"qdrant_upsert":  true,
	"memory_store":   true,
	"scratchpad_add": true,
}

// isVectorStoreOp returns true if the observation name suggests a vector
// store write operation.
func isVectorStoreOp(name string) bool {
	if vectorStoreTools[name] {
		return true
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "vector") ||
		strings.Contains(lower, "embedding") ||
		strings.Contains(lower, "upsert") ||
		(strings.Contains(lower, "memory") && strings.Contains(lower, "add"))
}

// buildTraceDetail constructs a TraceDetail from a Trace and its observations.
func buildTraceDetail(t Trace, observations []Observation) TraceDetail {
	sessionID := ""
	if t.SessionID != nil {
		sessionID = *t.SessionID
	}

	var outputPtr *string
	if t.Output != nil {
		s := truncateAny(t.Output, 50000)
		outputPtr = &s
	}

	detail := TraceDetail{
		TraceID:   t.ID,
		AgentName: t.Name,
		UserID:    t.UserID,
		SessionID: sessionID,
		Timestamp: t.Timestamp,
		Input:     truncateAny(t.Input, 50000),
		Output:    outputPtr,
	}

	if len(observations) == 0 {
		return detail
	}

	// Build a lookup map for parent resolution.
	obsMap := make(map[string]Observation, len(observations))
	for _, obs := range observations {
		obsMap[obs.ID] = obs
	}

	// Identify sub-agents: spans that have child generations.
	childGenerations := make(map[string]int) // parentID → count of GENERATION children
	subAgentSpans := make(map[string]bool)   // observation IDs that are sub-agent roots

	for _, obs := range observations {
		if obs.ParentObservationID == nil {
			continue
		}
		if obs.Type == "GENERATION" {
			childGenerations[*obs.ParentObservationID]++
		}
	}

	// A span with child generations is a sub-agent.
	for _, obs := range observations {
		if obs.Type != "SPAN" {
			continue
		}
		if childGenerations[obs.ID] > 0 {
			subAgentSpans[obs.ID] = true
		}
	}

	// isDescendantOfSubAgent walks the ancestor chain via obsMap to check
	// if any ancestor is a sub-agent root.
	isDescendantOfSubAgent := func(obs Observation) bool {
		parentID := obs.ParentObservationID
		for parentID != nil {
			if subAgentSpans[*parentID] {
				return true
			}
			parentObs, ok := obsMap[*parentID]
			if !ok {
				return false
			}
			parentID = parentObs.ParentObservationID
		}
		return false
	}

	// Count tool calls (all SPAN descendants of each sub-agent, recursively).
	subAgentToolCounts := make(map[string]int)
	for _, obs := range observations {
		if obs.Type != "SPAN" || obs.Name == "" {
			continue
		}
		if subAgentSpans[obs.ID] {
			continue // Don't count the sub-agent root itself.
		}
		// Walk up to find the nearest sub-agent ancestor.
		parentID := obs.ParentObservationID
		for parentID != nil {
			if subAgentSpans[*parentID] {
				subAgentToolCounts[*parentID]++
				break
			}
			parentObs, ok := obsMap[*parentID]
			if !ok {
				break
			}
			parentID = parentObs.ParentObservationID
		}
	}

	// Process observations.
	for _, obs := range observations {
		// Count vector store ops for ALL spans (including those under sub-agents).
		if obs.Type == "SPAN" && isVectorStoreOp(obs.Name) {
			detail.VectorStoreOps++
		}

		switch obs.Type {
		case "GENERATION":
			detail.LLMCalls++
			if obs.Usage != nil {
				detail.InputTokens += obs.Usage.Input
				detail.OutputTokens += obs.Usage.Output
				detail.TotalCost += obs.Usage.Cost
			}

		case "SPAN":
			if subAgentSpans[obs.ID] {
				// This span is a sub-agent root.
				sa := SubAgentDetail{
					Name:      obs.Name,
					Input:     truncateAny(obs.Input, 500),
					Output:    truncateAny(obs.Output, 500),
					LLMCalls:  childGenerations[obs.ID],
					ToolCalls: subAgentToolCounts[obs.ID],
				}
				detail.SubAgents = append(detail.SubAgents, sa)
				continue
			}

			// Skip root-level container spans (no parent).
			if obs.ParentObservationID == nil {
				continue
			}

			// Skip spans that are descendants of a sub-agent (they belong to
			// the sub-agent's execution tree, not top-level).
			if isDescendantOfSubAgent(obs) {
				continue
			}

			// Regular tool call span.
			if obs.Name != "" {
				parentName := ""
				if parent, ok := obsMap[*obs.ParentObservationID]; ok {
					parentName = parent.Name
				}
				tc := ToolCallDetail{
					Name:       obs.Name,
					Input:      truncateAny(obs.Input, 300),
					Output:     truncateAny(obs.Output, 300),
					ParentName: parentName,
				}
				detail.ToolCalls = append(detail.ToolCalls, tc)
			}
		}
	}

	// Compute duration from first to last observation.
	if len(observations) > 0 {
		earliest := observations[0].StartTime
		latest := observations[0].StartTime
		for _, obs := range observations[1:] {
			if obs.StartTime.Before(earliest) {
				earliest = obs.StartTime
			}
			if obs.EndTime != nil && obs.EndTime.After(latest) {
				latest = *obs.EndTime
			}
			if obs.StartTime.After(latest) {
				latest = obs.StartTime
			}
		}
		detail.Duration = latest.Sub(earliest)
	}

	return detail
}

// aggregate sums per-trace metrics into the result totals.
func (r *TraceAnalysisResult) aggregate() {
	for _, d := range r.TraceDetails {
		r.TotalToolCalls += len(d.ToolCalls)
		r.TotalLLMCalls += d.LLMCalls
		r.TotalSubAgents += len(d.SubAgents)
		r.TotalVectorStoreOps += d.VectorStoreOps
		r.TotalInputTokens += d.InputTokens
		r.TotalOutputTokens += d.OutputTokens
	}
}

// --- Formatting ---

// FormatReport generates a human-readable report of the trace analysis.
func (r TraceAnalysisResult) FormatReport() string {
	var sb strings.Builder
	sb.WriteString("# Trace Analysis Report\n\n")
	sb.WriteString("## Summary\n\n")
	fmt.Fprintf(&sb, "| Metric | Value |\n")
	fmt.Fprintf(&sb, "|--------|-------|\n")
	fmt.Fprintf(&sb, "| Traces analyzed | %d |\n", r.TracesAnalyzed)
	fmt.Fprintf(&sb, "| Total LLM calls | %d |\n", r.TotalLLMCalls)
	fmt.Fprintf(&sb, "| Total tool calls | %d |\n", r.TotalToolCalls)
	fmt.Fprintf(&sb, "| Total sub-agents | %d |\n", r.TotalSubAgents)
	fmt.Fprintf(&sb, "| Total vector store ops | %d |\n", r.TotalVectorStoreOps)
	fmt.Fprintf(&sb, "| Total input tokens | %d |\n", r.TotalInputTokens)
	fmt.Fprintf(&sb, "| Total output tokens | %d |\n\n", r.TotalOutputTokens)

	for i, d := range r.TraceDetails {
		fmt.Fprintf(&sb, "## Trace %d: %s\n\n", i+1, d.TraceID)
		fmt.Fprintf(&sb, "- **Agent**: %s\n", d.AgentName)
		fmt.Fprintf(&sb, "- **User**: %s\n", d.UserID)
		if d.SessionID != "" {
			fmt.Fprintf(&sb, "- **Session**: %s\n", d.SessionID)
		}
		fmt.Fprintf(&sb, "- **Time**: %s\n", d.Timestamp.Format(time.RFC3339))
		fmt.Fprintf(&sb, "- **Request**: %q\n", d.Input)
		if d.Output != nil {
			fmt.Fprintf(&sb, "- **Output**: %q\n", truncateStr(*d.Output, 200))
		} else {
			sb.WriteString("- **Output**: _(no output)_\n")
		}
		if d.Duration > 0 {
			fmt.Fprintf(&sb, "- **Duration**: %s\n", d.Duration.Round(time.Millisecond))
		}
		fmt.Fprintf(&sb, "- **LLM calls**: %d\n", d.LLMCalls)
		fmt.Fprintf(&sb, "- **Tool calls**: %d\n", len(d.ToolCalls))
		fmt.Fprintf(&sb, "- **Sub-agents**: %d\n", len(d.SubAgents))
		fmt.Fprintf(&sb, "- **Vector store ops**: %d\n", d.VectorStoreOps)
		fmt.Fprintf(&sb, "- **Input tokens**: %d\n", d.InputTokens)
		fmt.Fprintf(&sb, "- **Output tokens**: %d\n", d.OutputTokens)
		if d.TotalCost > 0 {
			fmt.Fprintf(&sb, "- **Cost**: $%.6f\n", d.TotalCost)
		}

		if len(d.ToolCalls) > 0 {
			sb.WriteString("\n### Tool Calls\n\n")
			sb.WriteString("| # | Tool | Parent | Input | Output |\n")
			sb.WriteString("|---|------|--------|-------|--------|\n")
			for j, tc := range d.ToolCalls {
				fmt.Fprintf(&sb, "| %d | %s | %s | %s | %s |\n",
					j+1, tc.Name, tc.ParentName,
					truncateStr(tc.Input, 60), truncateStr(tc.Output, 60))
			}
		}

		if len(d.SubAgents) > 0 {
			sb.WriteString("\n### Sub-Agents\n\n")
			for j, sa := range d.SubAgents {
				fmt.Fprintf(&sb, "**%d. %s** (LLM calls: %d, Tool calls: %d)\n",
					j+1, sa.Name, sa.LLMCalls, sa.ToolCalls)
				if sa.Input != "" {
					fmt.Fprintf(&sb, "- Input: %s\n", sa.Input)
				}
				if sa.Output != "" {
					fmt.Fprintf(&sb, "- Output: %s\n", sa.Output)
				}
			}
		}
		sb.WriteString("\n---\n\n")
	}

	return sb.String()
}

// --- Helpers ---

// truncateAny converts any value to a truncated string representation.
func truncateAny(v any, maxRunes int) string {
	if v == nil {
		return ""
	}
	var s string
	switch val := v.(type) {
	case string:
		s = val
	default:
		b, err := json.Marshal(val)
		if err != nil {
			s = fmt.Sprintf("%v", val)
		} else {
			s = string(b)
		}
	}
	return truncateStr(s, maxRunes)
}

// truncateStr truncates a string to maxRunes.
func truncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
