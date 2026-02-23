// Package metrics provides monitoring and observability query tools for
// agents. It enables agents to check system health, query time-series
// metrics, and inspect alerting state by talking to Prometheus-compatible
// APIs.
//
// Problem: When agents handle incident response or answer questions like
// "Is the API healthy?" or "What's the p99 latency?", they need access to
// real-time metrics. Without this tool, agents must ask humans to check
// dashboards manually, adding minutes to incident resolution.
//
// Supported operations:
//   - instant_query — execute a PromQL query at a point in time
//   - range_query — execute a PromQL query over a time range
//   - series — find time series matching label selectors
//   - labels — list all label names or values for a label
//   - alerts — list currently firing alerts
//   - targets — list monitored scrape targets and their health
//
// Safety guards:
//   - 30-second HTTP timeout
//   - Output truncated at 32 KB
//   - Read-only: no mutations to metrics or alert rules
//   - URL validation: only HTTP/HTTPS endpoints allowed
//
// Dependencies:
//   - Go stdlib only (net/http, encoding/json)
//   - A Prometheus-compatible endpoint (Prometheus, Thanos, VictoriaMetrics, Grafana Mimir)
//   - PROMETHEUS_URL env var (defaults to http://localhost:9090)
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxOutputBytes = 32 << 10
	apiTimeout     = 30 * time.Second
)

// ────────────────────── Request / Response ──────────────────────

type metricsRequest struct {
	Operation string `json:"operation" jsonschema:"description=Metrics operation.,enum=instant_query,enum=range_query,enum=series,enum=labels,enum=alerts,enum=targets"`
	// For queries:
	Query string `json:"query,omitempty" jsonschema:"description=PromQL query expression (e.g. up, rate(http_requests_total[5m]), histogram_quantile(0.99, ...))."`
	// For range_query:
	Start string `json:"start,omitempty" jsonschema:"description=Range start time. RFC3339 (e.g. 2025-01-15T00:00:00Z) or relative duration like '1h' (1 hour ago), '7d' (7 days ago). Defaults to 1 hour ago."`
	End   string `json:"end,omitempty" jsonschema:"description=Range end time. RFC3339 or relative duration. Defaults to now."`
	Step  string `json:"step,omitempty" jsonschema:"description=Query step (e.g. 15s, 1m, 5m). Defaults to 1m."`
	// For series:
	Match string `json:"match,omitempty" jsonschema:"description=Series selector (e.g. {job='api-server'}). Required for series operation."`
	// For labels:
	LabelName string `json:"label_name,omitempty" jsonschema:"description=Label name to list values for (e.g. 'job', 'instance'). If empty, lists all label names."`
	// Connection:
	PrometheusURL string `json:"prometheus_url,omitempty" jsonschema:"description=Prometheus API base URL. Defaults to PROMETHEUS_URL env var or http://localhost:9090."`
}

// op returns the normalized operation name.
func (r metricsRequest) op() string {
	return strings.ToLower(strings.TrimSpace(r.Operation))
}

// baseURL resolves the Prometheus endpoint: request field → env var → default.
func (r metricsRequest) baseURL() string {
	if r.PrometheusURL != "" {
		return strings.TrimRight(r.PrometheusURL, "/")
	}
	if u := os.Getenv("PROMETHEUS_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:9090"
}

// validateBaseURL checks that the resolved base URL uses HTTP or HTTPS
// and has a non-empty host.
func (r metricsRequest) validateBaseURL() error {
	raw := r.baseURL()
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid Prometheus URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid Prometheus URL %q: only http and https schemes are allowed", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid Prometheus URL %q: host is required", raw)
	}
	return nil
}

// requireQuery validates that a PromQL query expression is provided.
func (r metricsRequest) requireQuery(op string) error {
	if r.Query == "" {
		return fmt.Errorf("query is required for %s", op)
	}
	return nil
}

type metricsResponse struct {
	Operation string `json:"operation"`
	Result    string `json:"result"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type metricsTools struct {
	client *http.Client
}

func newMetricsTools() *metricsTools {
	return &metricsTools{
		client: &http.Client{Timeout: apiTimeout},
	}
}

func (m *metricsTools) metricsTool() tool.CallableTool {
	return function.NewFunctionTool(
		m.query,
		function.WithName("util_metrics"),
		function.WithDescription(
			"Query monitoring metrics from Prometheus. Supported operations: "+
				"instant_query (execute PromQL at current time), "+
				"range_query (execute PromQL over a time range), "+
				"series (find time series matching label selectors), "+
				"labels (list label names or values), "+
				"alerts (list currently firing alerts), "+
				"targets (list scrape targets and their health). "+
				"Uses a Prometheus-compatible API endpoint.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (m *metricsTools) query(ctx context.Context, req metricsRequest) (metricsResponse, error) {
	op := req.op()
	resp := metricsResponse{Operation: op}

	if err := req.validateBaseURL(); err != nil {
		return resp, err
	}
	baseURL := req.baseURL()

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	switch op {
	case "instant_query":
		return m.instantQuery(ctx, req, resp, baseURL)
	case "range_query":
		return m.rangeQuery(ctx, req, resp, baseURL)
	case "series":
		return m.series(ctx, req, resp, baseURL)
	case "labels":
		return m.labels(ctx, req, resp, baseURL)
	case "alerts":
		return m.alerts(ctx, resp, baseURL)
	case "targets":
		return m.targets(ctx, resp, baseURL)
	default:
		return resp, fmt.Errorf("unsupported operation %q: use instant_query, range_query, series, labels, alerts, or targets", op)
	}
}

func (m *metricsTools) instantQuery(ctx context.Context, req metricsRequest, resp metricsResponse, baseURL string) (metricsResponse, error) {
	if err := req.requireQuery("instant_query"); err != nil {
		return resp, err
	}
	params := url.Values{"query": {req.Query}}
	return m.doGet(ctx, baseURL+"/api/v1/query", params, resp)
}

func (m *metricsTools) rangeQuery(ctx context.Context, req metricsRequest, resp metricsResponse, baseURL string) (metricsResponse, error) {
	if err := req.requireQuery("range_query"); err != nil {
		return resp, err
	}

	now := time.Now()
	start := now.Add(-1 * time.Hour).Format(time.RFC3339)
	end := now.Format(time.RFC3339)
	step := "1m"

	if req.Start != "" {
		start = resolveTimeOrRelative(req.Start, now)
	}
	if req.End != "" {
		end = resolveTimeOrRelative(req.End, now)
	}
	if req.Step != "" {
		step = req.Step
	}

	params := url.Values{
		"query": {req.Query},
		"start": {start},
		"end":   {end},
		"step":  {step},
	}
	return m.doGet(ctx, baseURL+"/api/v1/query_range", params, resp)
}

func (m *metricsTools) series(ctx context.Context, req metricsRequest, resp metricsResponse, baseURL string) (metricsResponse, error) {
	sel := req.Match
	if sel == "" {
		sel = req.Query
	}
	if sel == "" {
		return resp, fmt.Errorf("match (or query) is required for series operation")
	}

	now := time.Now()
	params := url.Values{
		"match[]": {sel},
		"start":   {now.Add(-1 * time.Hour).Format(time.RFC3339)},
		"end":     {now.Format(time.RFC3339)},
	}
	return m.doGet(ctx, baseURL+"/api/v1/series", params, resp)
}

func (m *metricsTools) labels(ctx context.Context, req metricsRequest, resp metricsResponse, baseURL string) (metricsResponse, error) {
	if req.LabelName != "" {
		return m.doGet(ctx, baseURL+"/api/v1/label/"+url.PathEscape(req.LabelName)+"/values", nil, resp)
	}
	return m.doGet(ctx, baseURL+"/api/v1/labels", nil, resp)
}

func (m *metricsTools) alerts(ctx context.Context, resp metricsResponse, baseURL string) (metricsResponse, error) {
	return m.doGet(ctx, baseURL+"/api/v1/alerts", nil, resp)
}

func (m *metricsTools) targets(ctx context.Context, resp metricsResponse, baseURL string) (metricsResponse, error) {
	return m.doGet(ctx, baseURL+"/api/v1/targets", nil, resp)
}

// doGet performs a GET request and formats the response.
func (m *metricsTools) doGet(ctx context.Context, rawURL string, params url.Values, resp metricsResponse) (metricsResponse, error) {
	if params != nil {
		rawURL += "?" + params.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return resp, fmt.Errorf("invalid URL: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		return resp, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, int64(maxOutputBytes*2)))
	if err != nil {
		return resp, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		resp.Status = fmt.Sprintf("HTTP %d", httpResp.StatusCode)
		resp.Result = string(body)
		resp.Message = fmt.Sprintf("Prometheus returned HTTP %d", httpResp.StatusCode)
		return resp, nil
	}

	// Pretty-print the JSON result.
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		resp.Result = string(body)
	} else {
		formatted, _ := json.MarshalIndent(raw, "", "  ")
		resp.Result = string(formatted)
	}

	if len(resp.Result) > maxOutputBytes {
		resp.Result = resp.Result[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}

	resp.Status = "success"
	resp.Message = fmt.Sprintf("Query returned %d bytes", len(resp.Result))
	return resp, nil
}

// resolveTimeOrRelative returns the raw string as-is if it parses as RFC3339.
// Otherwise it tries to interpret it as a relative duration (e.g. "1h", "30m",
// "7d") and subtracts that from `now`. On failure, the original string is
// returned verbatim and Prometheus will report the parse error.
func resolveTimeOrRelative(raw string, now time.Time) string {
	raw = strings.TrimSpace(raw)
	// Try RFC3339 first.
	if _, err := time.Parse(time.RFC3339, raw); err == nil {
		return raw
	}
	// Try relative durations. Support "d" (days) and "w" (weeks) manually.
	s := strings.ToLower(raw)
	if strings.HasSuffix(s, "d") {
		if n, err := fmt.Sscanf(s, "%d", new(int)); err == nil && n == 1 {
			var days int
			fmt.Sscanf(s, "%d", &days)
			return now.Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
		}
	}
	if strings.HasSuffix(s, "w") {
		if n, err := fmt.Sscanf(s, "%d", new(int)); err == nil && n == 1 {
			var weeks int
			fmt.Sscanf(s, "%d", &weeks)
			return now.Add(-time.Duration(weeks) * 7 * 24 * time.Hour).Format(time.RFC3339)
		}
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return now.Add(-d).Format(time.RFC3339)
	}
	return raw // pass through — Prometheus will report the error
}
