# Incident Response Runbook

Cross-domain incident triage combining observability, infrastructure, and collaboration tools.

---

## Triage Decision Tree

```
User reports incident
        │
        ▼
┌───────────────────────┐
│  1. Vital Signs Check │  ← Single batched command (K8s + Cloud + Observability)
│     Is anything down? │
└───────┬───────────────┘
        │
   ┌────┴────┐
   ▼         ▼
Cloud?     App/K8s?
   │         │
   ▼         ▼
┌────────┐ ┌──────────┐
│ Check  │ │ Check    │
│ AWS/GCP│ │ pods,    │
│ health │ │ logs,    │
│ events │ │ metrics  │
└───┬────┘ └────┬─────┘
    │           │
    └─────┬─────┘
          ▼
   ┌──────────────┐
   │ Correlate    │  ← Metrics + Logs + Traces + Recent Changes
   │ Root Cause   │
   └──────┬───────┘
          ▼
   ┌──────────────┐
   │ Remediate    │  ← Rollback, scale, restart (with confirmation)
   └──────────────┘
```

---

## Step 1: Vital Signs Check

Run this **directly** (no sub-agent needed) to immediately see if the problem is Cloud or App:

```bash
NS="<namespace>"
CLUSTER="<cluster-name>"
REGION="<region>"

echo "=== K8S STATUS ===" && \
kubectl get pods -n $NS -o wide && \
kubectl get events -n $NS --sort-by='.lastTimestamp' | tail -10 && \
echo "=== CLOUD HEALTH ===" && \
aws eks describe-cluster --name $CLUSTER --region $REGION --query 'cluster.{status:status,health:health}' 2>/dev/null && \
echo "=== ACTIVE INCIDENTS ===" && \
aws health describe-events --filter "eventStatusCodes=open" --region $REGION --max-items 5 2>/dev/null
```

---

## Step 2: Parallel Deep Dive (Spawn Sub-Agents)

Once you know the impact area, spawn parallel sub-agents:

```
# Sub-Agent 1: Metrics investigation
create_agent(
  goal="Query Prometheus/Grafana for the following in namespace <NS> over the last 2 hours:
    1. Error rate by service: rate(http_requests_total{code=~'5..'}[5m])
    2. Latency P99: histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))
    3. CPU throttling: container_cpu_cfs_throttled_periods_total
    Report which services show anomalies.",
  task_type="tool_calling",
  max_tool_iterations=30,
  max_llm_calls=40,
  mcp_server_names=["grafana"]
)

# Sub-Agent 2: Log analysis
create_agent(
  goal="Search logs for errors in the last 2 hours:
    1. kubectl logs -n <NS> --selector app=<service> --since=2h | grep -i 'error\\|panic\\|fatal'
    2. Check for OOMKilled: kubectl get events -n <NS> --field-selector reason=OOMKilling
    3. Check for CrashLoopBackOff: kubectl get pods -n <NS> | grep -i crash
    Report the most frequent error patterns.",
  task_type="tool_calling",
  max_tool_iterations=30,
  max_llm_calls=40
)

# Sub-Agent 3: Infrastructure check
create_agent(
  goal="Check infrastructure health:
    1. Node status: kubectl get nodes
    2. Resource pressure: kubectl describe nodes | grep -A5 Conditions
    3. AWS/Cloud events: check for EC2 status issues, scheduled maintenance
    Report any infrastructure anomalies.",
  task_type="tool_calling",
  max_tool_iterations=30,
  max_llm_calls=40
)
```

---

## Step 3: Correlation Matrix

| Symptom | Check Metrics | Check Logs | Check Infra |
|---|---|---|---|
| **High latency** | `http_request_duration` spikes | Slow query logs, timeout errors | Node CPU/memory, disk I/O |
| **5xx errors** | `http_requests_total{code=~"5.."}` | Application stack traces | Pod OOMKilled, CrashLoop |
| **Connection errors** | Connection pool saturation | Connection refused/reset logs | Security groups, DNS, service mesh |
| **Pods not starting** | — | Container image pull errors | Node capacity, resource quotas |
| **Data inconsistency** | — | Database error logs | Replication lag, storage issues |

---

## Step 4: Common Remediation Actions

> **⚠️ Always confirm with user before executing write operations**

### Rollback Deployment

```bash
# ArgoCD rollback
argocd app history <app-name>
argocd app rollback <app-name> <revision>

# Kubernetes rollback
kubectl rollout undo deployment/<name> -n <namespace>
kubectl rollout status deployment/<name> -n <namespace>
```

### Scale Up

```bash
# Horizontal scaling
kubectl scale deployment/<name> -n <namespace> --replicas=<count>

# HPA check
kubectl get hpa -n <namespace>
```

### Restart Pods

```bash
# Rolling restart
kubectl rollout restart deployment/<name> -n <namespace>
```

---

## Step 5: Post-Incident

After stabilization:

1. **Timeline** — document what happened and when
2. **Root cause** — identify the underlying issue
3. **Impact** — quantify affected users/services/duration
4. **Action items** — preventive measures
5. **Jira ticket** — create a post-mortem ticket (if Atlassian MCP configured)

```bash
# Create post-mortem issue (via gh CLI)
gh issue create --title "Post-mortem: <incident-summary>" \
  --body "## Timeline\n...\n## Root Cause\n...\n## Action Items\n..."
```
