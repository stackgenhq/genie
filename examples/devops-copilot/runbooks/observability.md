# Observability Runbook

Reference guide for Grafana, Prometheus, Loki, and trace analysis.

---

## Grafana (via MCP or API)

### Dashboard Operations

```bash
# List all dashboards
# Via MCP: use grafana MCP tools (list_dashboards, get_dashboard, etc.)
# Via CLI: curl against Grafana API
curl -s -H "Authorization: Bearer $GRAFANA_API_KEY" \
  "$GRAFANA_URL/api/search?type=dash-db" | jq '.[] | {title, uid, url}'
```

### Alert Management

```bash
# List firing alerts
curl -s -H "Authorization: Bearer $GRAFANA_API_KEY" \
  "$GRAFANA_URL/api/alertmanager/grafana/api/v2/alerts" | jq '.[] | select(.status.state=="active")'

# Get alert rule details
curl -s -H "Authorization: Bearer $GRAFANA_API_KEY" \
  "$GRAFANA_URL/api/v1/provisioning/alert-rules" | jq '.'
```

### Dashboard URLs

Template for linking users to dashboards:
```
<Grafana URL>/d/<dashboard-uid>/<dashboard-slug>?orgId=1
<Grafana URL>/alerting/list                           # All alerts
<Grafana URL>/alerting/grafana/<alert-id>/view         # Specific alert
<Grafana URL>/explore                                  # Ad-hoc queries
```

---

## Prometheus

### Query Patterns

```bash
# Instant query
curl -s "$PROMETHEUS_URL/api/v1/query?query=up" | jq '.data.result[]'

# Range query (last 1 hour, 5m step)
curl -s "$PROMETHEUS_URL/api/v1/query_range?query=rate(http_requests_total[5m])&start=$(date -u -d '1 hour ago' +%s)&end=$(date -u +%s)&step=300"

# Search available metrics
curl -s "$PROMETHEUS_URL/api/v1/label/__name__/values" | jq '.data[]' | grep -i "<keyword>"

# Validate a PromQL query
promtool promql lint "rate(http_requests_total[5m])"
```

### Common PromQL Patterns

| Use Case | Query |
|---|---|
| Error rate | `rate(http_requests_total{code=~"5.."}[5m]) / rate(http_requests_total[5m])` |
| P99 latency | `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))` |
| CPU usage % | `100 - (avg by(instance)(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)` |
| Memory usage | `node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes` |
| Disk usage | `1 - (node_filesystem_avail_bytes / node_filesystem_size_bytes)` |
| Request rate | `sum(rate(http_requests_total[5m])) by (service)` |

### Alert Rules

```bash
# List active alerts
curl -s "$PROMETHEUS_URL/api/v1/alerts" | jq '.data.alerts[] | {alertname: .labels.alertname, state: .state, severity: .labels.severity}'

# Check alert rule config
curl -s "$PROMETHEUS_URL/api/v1/rules" | jq '.data.groups[].rules[] | select(.type=="alerting")'
```

---

## Loki (Log Analysis)

### Query Patterns

```bash
# Recent logs for a service
logcli query '{app="<service-name>"}' --limit=100 --since=1h

# Error logs
logcli query '{app="<service-name>"} |= "error"' --limit=50 --since=30m

# JSON-structured logs with field filtering
logcli query '{app="<service-name>"} | json | level="error"' --limit=50

# Label discovery
logcli labels
logcli labels app
```

### Via Grafana API

```bash
# Query logs through Grafana
curl -s -H "Authorization: Bearer $GRAFANA_API_KEY" \
  "$GRAFANA_URL/api/ds/query" -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "queries": [{
      "refId": "A",
      "datasourceId": <loki-datasource-id>,
      "expr": "{app=\"<service-name>\"} |= \"error\"",
      "maxLines": 100
    }]
  }'
```

---

## Trace Analysis

For services instrumented with OpenTelemetry:

### ClickHouse APM (if using ObserveNow/SigNoz-style)

```sql
-- P90 latency by service (last 1 hour)
SELECT service, quantilesMerge(0.9)(quantilesDuration)[1] as p90_ms
FROM jaeger_apm_local_aggregated_v2
WHERE timestamp > now() - INTERVAL 1 HOUR
GROUP BY service ORDER BY p90_ms DESC;

-- Error rate by service
SELECT service,
  countIf(hasError = true) * 100.0 / count() as error_pct
FROM jaeger_apm_local_v2
WHERE timestamp > now() - INTERVAL 1 HOUR
GROUP BY service ORDER BY error_pct DESC;

-- Slow operations
SELECT service, operation, avg(durationNano) / 1e6 as avg_ms
FROM jaeger_apm_local_v2
WHERE timestamp > now() - INTERVAL 1 HOUR
GROUP BY service, operation ORDER BY avg_ms DESC LIMIT 20;
```

---

## Correlation Workflow

When investigating an issue, always check all three pillars:

1. **Metrics first** — What changed? Query Prometheus/Grafana for error rate spikes, latency anomalies
2. **Logs second** — What errors? Query Loki/Elasticsearch for error messages, stack traces
3. **Traces third** — Where's the bottleneck? Find slow spans, failing dependencies
4. **Infrastructure** — Is it a resource issue? Check K8s pods, cloud resources
5. **Recent deployments** — Was something changed? Check ArgoCD, Git history

### Data Formatting Rules

- Convert all timestamps to human-readable (`2024-02-08T14:30:00Z`, not epoch)
- Convert bytes to MB/GB, microseconds to ms/s
- Round percentages to 2 decimal places
- Always include the Grafana explore link for queries you run
