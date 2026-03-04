---
name: incident-investigation
description: Cross-platform incident investigation correlating metrics, logs, traces, and infrastructure to identify root cause
---

# Incident Investigation

Investigate a production incident by correlating data across observability platforms (metrics, logs, traces) and infrastructure (Kubernetes, cloud) to identify root cause and suggest remediation.

## When to Use This Skill

Use this skill when:
- A production incident is reported (high error rates, latency spikes, outages)
- An alert has fired and needs investigation
- Post-mortem root cause analysis is needed
- Performance degradation needs diagnosis

## Prerequisites

- At least one observability integration (Grafana, Datadog, or CLI-accessible Prometheus/Loki)
- Shell access to Kubernetes (kubectl) and/or cloud CLIs (aws, gcloud, az)

## Workflow

### Step 1: Scope the Incident

Gather context from the user:
- **What service(s)** are affected?
- **When** did it start? (timestamp or "last N hours")
- **What symptoms** are observed? (errors, latency, downtime)
- **What namespace/cluster/region** is involved?

### Step 2: Check Metrics (Error Rate, Latency, Throughput)

```bash
# Via Grafana MCP or Prometheus API
# Error rate spike
rate(http_requests_total{service="<name>",code=~"5.."}[5m])

# Latency spike (P99)
histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{service="<name>"}[5m]))

# Throughput change
sum(rate(http_requests_total{service="<name>"}[5m]))

# Infrastructure metrics
container_cpu_usage_seconds_total{container="<name>"}
container_memory_usage_bytes{container="<name>"}
```

### Step 3: Check Logs

```bash
# Kubernetes pod logs
kubectl logs -n <namespace> --selector app=<service> --since=2h --tail=200 | grep -iE "error|panic|fatal|timeout|refused"

# Via Loki/logcli
logcli query '{app="<service>"}|="error"' --limit=100 --since=2h

# Via Grafana MCP: use explore_logs tool with appropriate query
```

### Step 4: Check Traces (if available)

```bash
# Via Datadog MCP: list_spans + get_trace
# Via ClickHouse (ObserveNow):
clickhouse-client --query "
  SELECT traceID, service, operation, durationNano/1e6 as duration_ms, hasError
  FROM jaeger_apm_local_v2
  WHERE service = '<service>' AND timestamp > now() - INTERVAL 2 HOUR
  ORDER BY durationNano DESC LIMIT 10
"
```

### Step 5: Check Infrastructure

```bash
# Kubernetes status
kubectl get pods -n <namespace> -o wide
kubectl get events -n <namespace> --sort-by='.lastTimestamp' | tail -20
kubectl top pods -n <namespace>

# Check for OOMKilled or CrashLoopBackOff
kubectl get pods -n <namespace> -o json | jq '.items[] | select(.status.containerStatuses[]?.state.waiting.reason == "CrashLoopBackOff" or .status.containerStatuses[]?.lastState.terminated.reason == "OOMKilled") | {name: .metadata.name, reason: .status.containerStatuses[].state.waiting.reason // .status.containerStatuses[].lastState.terminated.reason}'
```

### Step 6: Check Recent Changes

```bash
# Git — recent deployments
git log --oneline --since="4 hours ago"

# ArgoCD — recent syncs
argocd app history <app-name>

# Kubernetes — recent rollouts
kubectl rollout history deployment/<name> -n <namespace>
```

### Step 7: Correlate and Report

After gathering data from all sources, correlate:

1. **Timeline** — when did metrics first show anomalies?
2. **Blast radius** — which services are affected?
3. **Root cause** — what changed? (deployment, config, infrastructure)
4. **Impact** — error count, affected users, duration
5. **Remediation** — rollback, scale, restart, fix

## Output

Present a structured incident report:
- **Summary**: One-line description of the incident
- **Timeline**: Chronological events
- **Root Cause**: What caused the issue
- **Impact**: Services affected, duration, error rates
- **Remediation**: Steps taken or recommended
- **Prevention**: Action items to prevent recurrence
