---
name: grafana-dashboard-builder
description: Build a Grafana dashboard by discovering available metrics, sampling them, and creating panels with golden signals
---

# Grafana Dashboard Builder

Build a Grafana dashboard for a service or team by discovering available metrics, sampling them for validity, and creating a dashboard with golden signals (latency, error rate, throughput, saturation).

## When to Use This Skill

Use this skill when:
- The user wants a new Grafana dashboard for a service or team
- Golden signals monitoring needs to be set up
- A service's key metrics need to be visualized

## Prerequisites

- Grafana MCP server configured, OR Grafana API URL and API key
- Prometheus datasource configured in Grafana

## Workflow

### Step 1: Discover Available Metrics

Use the Grafana MCP server (preferred) or Prometheus API to find metrics for the target service:

```bash
# Via Prometheus API — search for metrics related to the service
curl -s "$PROMETHEUS_URL/api/v1/label/__name__/values" | jq '.data[]' | grep -iE "<service-name>|http|request|latency|error|cpu|memory"

# Via Grafana MCP: use search_metrics or list_datasources tool
# Look for metrics matching patterns:
#   - HTTP requests: http_requests_total, http_request_duration_seconds_bucket
#   - Errors: http_requests_total{code=~"5.."}
#   - CPU: container_cpu_usage_seconds_total, process_cpu_seconds_total
#   - Memory: container_memory_usage_bytes, process_resident_memory_bytes
```

### Step 2: Sample Metrics for Validity

```bash
# Test that metrics actually have data (instant query)
curl -s "$PROMETHEUS_URL/api/v1/query?query=http_requests_total{service=\"<service-name>\"}" | jq '.data.result | length'

# Sample over 24h to confirm data continuity
curl -s "$PROMETHEUS_URL/api/v1/query_range?query=rate(http_requests_total{service=\"<service-name>\"}[5m])&start=$(date -u -d '24 hours ago' +%s)&end=$(date -u +%s)&step=3600" | jq '.data.result | length'
```

### Step 3: Build Dashboard Panels

Create panels for the four golden signals:

1. **Latency** — P50, P90, P99 request duration
   ```
   histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{service="<name>"}[5m]))
   ```

2. **Error Rate** — percentage of 5xx responses
   ```
   rate(http_requests_total{service="<name>",code=~"5.."}[5m]) / rate(http_requests_total{service="<name>"}[5m]) * 100
   ```

3. **Throughput** — requests per second
   ```
   sum(rate(http_requests_total{service="<name>"}[5m]))
   ```

4. **Saturation** — CPU and memory usage
   ```
   rate(container_cpu_usage_seconds_total{container="<name>"}[5m])
   container_memory_usage_bytes{container="<name>"}
   ```

### Step 4: Create the Dashboard

Use the Grafana MCP server's `create_dashboard` tool or the API:

```bash
# Via Grafana API
curl -s -X POST "$GRAFANA_URL/api/dashboards/db" \
  -H "Authorization: Bearer $GRAFANA_API_KEY" \
  -H "Content-Type: application/json" \
  -d @- <<'EOF'
{
  "dashboard": {
    "title": "<Service Name> - Golden Signals",
    "panels": [
      {"title": "Request Latency (P99)", "type": "timeseries", ...},
      {"title": "Error Rate (%)", "type": "timeseries", ...},
      {"title": "Request Throughput (RPS)", "type": "timeseries", ...},
      {"title": "CPU Usage", "type": "timeseries", ...},
      {"title": "Memory Usage", "type": "timeseries", ...}
    ],
    "templating": {
      "list": [{"name": "service", "type": "query", ...}]
    }
  },
  "overwrite": false
}
EOF
```

### Step 5: Report Dashboard URL

After creation, provide:
- The direct dashboard URL
- A summary of panels created
- Suggestions for additional panels (optional)

## Output

- Grafana dashboard URL
- List of panels with their PromQL queries
- Recommendations for alerts based on the same metrics
