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
	"strconv"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
)

// AgentUsageStats holds the aggregated token usage and cost statistics for a
// single agent within a given time window. Without this type, callers would
// have to parse raw Langfuse API responses and perform their own aggregation,
// which is error-prone and duplicated across consumers.
type AgentUsageStats struct {
	// AgentName is the trace name (i.e. the agent identifier) in Langfuse.
	AgentName string `json:"agent_name"`
	// TotalCost is the total cost in USD for all observations belonging to
	// this agent's traces within the queried time window.
	TotalCost float64 `json:"total_cost"`
	// InputTokens is the sum of input tokens consumed across all observations.
	InputTokens float64 `json:"input_tokens"`
	// OutputTokens is the sum of output tokens produced across all observations.
	OutputTokens float64 `json:"output_tokens"`
	// TotalTokens is the sum of all tokens consumed across all observations.
	TotalTokens float64 `json:"total_tokens"`
	// Count is the total number of observations for this agent.
	Count float64 `json:"count"`
}

// GetAgentStatsRequest encapsulates the parameters for querying agent-level
// usage statistics from Langfuse. The Duration field specifies the lookback
// window from the current time. An optional AgentName can filter results to a
// single agent.
type GetAgentStatsRequest struct {
	// Duration is the lookback period from now (e.g. 24h, 7*24h).
	Duration time.Duration
	// AgentName optionally restricts the query to a single agent.
	// When empty, stats for all agents are returned.
	AgentName string
}

// metricsQuery is the JSON body sent to the Langfuse v1 metrics endpoint
// (/api/public/metrics) as a URL-encoded query parameter.
type metricsQuery struct {
	View          string             `json:"view"`
	Metrics       []metricsMetric    `json:"metrics"`
	Dimensions    []metricsDimension `json:"dimensions"`
	Filters       []metricsFilter    `json:"filters,omitempty"`
	FromTimestamp string             `json:"fromTimestamp"`
	ToTimestamp   string             `json:"toTimestamp"`
}

type metricsMetric struct {
	Measure     string `json:"measure"`
	Aggregation string `json:"aggregation"`
}

type metricsDimension struct {
	Field string `json:"field"`
}

type metricsFilter struct {
	Column   string `json:"column"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
	Type     string `json:"type"`
}

// metricsResponse mirrors the Langfuse MetricsResponse schema. Each data
// item contains the requested metric values keyed by
// "<aggregation>_<measure>" (e.g. "sum_totalCost") plus the dimension values.
// Note: the v1 API returns token values as strings and cost values as
// nullable floats.
type metricsResponse struct {
	Data []map[string]any `json:"data"`
}

// GetAgentStats queries the Langfuse Metrics v1 API to return per-agent token
// usage and cost statistics. It uses the "observations" view grouped by
// "traceName" which maps to the agent name set during trace creation.
//
// We use the v1 endpoint (/api/public/metrics) instead of v2 because v2 is
// only available on Langfuse Cloud and not on self-hosted instances.
//
// This method exists so that consumers can programmatically monitor agent costs
// and token budgets. Without it, operators would need to manually query the
// Langfuse UI or write bespoke HTTP calls against the Langfuse API.
func (c *client) GetAgentStats(ctx context.Context, req GetAgentStatsRequest) ([]AgentUsageStats, error) {
	logr := logger.GetLogger(ctx).With("fn", "langfuse.GetAgentStats")

	if req.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %s", req.Duration)
	}

	now := time.Now().UTC()
	from := now.Add(-req.Duration)

	query := metricsQuery{
		View: "observations",
		Metrics: []metricsMetric{
			{Measure: "totalCost", Aggregation: "sum"},
			{Measure: "inputTokens", Aggregation: "sum"},
			{Measure: "outputTokens", Aggregation: "sum"},
			{Measure: "totalTokens", Aggregation: "sum"},
			{Measure: "count", Aggregation: "count"},
		},
		Dimensions: []metricsDimension{
			{Field: "traceName"},
		},
		FromTimestamp: from.Format(time.RFC3339),
		ToTimestamp:   now.Format(time.RFC3339),
	}

	if req.AgentName != "" {
		query.Filters = []metricsFilter{
			{
				Column:   "traceName",
				Operator: "=",
				Value:    req.AgentName,
				Type:     "string",
			},
		}
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metrics query: %w", err)
	}

	apiURL := fmt.Sprintf("%s/api/public/metrics?query=%s",
		c.config.langfuseHost(), url.QueryEscape(string(queryJSON)))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics request: %w", err)
	}

	logr.Debug("fetching agent stats", "from", from.Format(time.RFC3339), "to", now.Format(time.RFC3339), "agentName", req.AgentName)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d from metrics API: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics response body: %w", err)
	}

	var metricsResp metricsResponse
	if err := json.Unmarshal(body, &metricsResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics response: %w", err)
	}

	return parseAgentStats(metricsResp.Data), nil
}

// parseAgentStats converts raw Langfuse v1 metrics response rows into typed
// AgentUsageStats. The v1 API uses keys in the format
// "<aggregation>_<measure>" (e.g. "sum_totalCost", "count_count") and returns
// token counts as strings and cost values as nullable floats.
func parseAgentStats(data []map[string]any) []AgentUsageStats {
	stats := make([]AgentUsageStats, 0, len(data))
	for _, row := range data {
		s := AgentUsageStats{
			AgentName:    stringFromMap(row, "traceName"),
			TotalCost:    numericFromMap(row, "sum_totalCost"),
			InputTokens:  numericFromMap(row, "sum_inputTokens"),
			OutputTokens: numericFromMap(row, "sum_outputTokens"),
			TotalTokens:  numericFromMap(row, "sum_totalTokens"),
			Count:        numericFromMap(row, "count_count"),
		}
		stats = append(stats, s)
	}
	return stats
}

// stringFromMap safely extracts a string value from a map.
func stringFromMap(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// numericFromMap safely extracts a numeric value from a map, handling both
// float64 (standard JSON number) and string representations (as returned by
// the Langfuse v1 metrics API for token counts). Returns 0 for nil/null values.
func numericFromMap(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}
