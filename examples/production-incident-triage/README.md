# Production Troubleshooting: Incident Triage with Multi-Agent

> **Audience:** Developers troubleshooting production issues  
> **Scenario:** A developer on-call receives a PagerDuty alert: HTTP 500 error rate has spiked on the `payment-service`. They need to quickly identify the root cause, correlate logs with recent deployments, and either roll back or hot-fix in minutes — not hours.

---

## The Problem

It's 2 AM. The payment service is returning 500s. You need to:

1. Check Kubernetes pod status and recent events
2. Pull error logs from the failing pods
3. Correlate the failure with the most recent Git deployment
4. Identify the root cause in the diff
5. Decide: rollback or hot-fix?
6. Execute the fix and verify

This normally requires switching between `kubectl`, GitHub, Datadog/Grafana, and your IDE. With Genie, you stay in one terminal and the multi-agent system runs these in parallel.

---

## Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                   CodeOwner (Incident Commander)                  │
│  Persona: Full developer with shell + file + MCP access          │
│  Memory:  Accumulates findings across investigation steps        │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Phase 1: Parallel Triage (3 sub-agents simultaneously)           │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────┐  │
│  │  Sub-Agent A  │  │  Sub-Agent B  │  │     Sub-Agent C        │  │
│  │  K8s Status   │  │  Git History  │  │   Log Analysis         │  │
│  │  + Pod Logs   │  │  + Diff       │  │   + Error Patterns     │  │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬─────────────┘  │
│         │                 │                      │                │
│  Phase 2: Root Cause Analysis (orchestrator synthesizes)          │
│                                                                   │
│  Phase 3: Fix or Rollback (sub-agent executes)                    │
│  ┌──────────────────────────────────────────────┐                │
│  │  Sub-Agent D: Apply fix + verify deployment   │                │
│  └──────────────────────────────────────────────┘                │
│                                                                   │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                    Tool Registry                           │  │
│  │  • Shell tool (kubectl, git, curl, jq)                     │  │
│  │  • File tools (read code, search for patterns)             │  │
│  │  • Skill: kubernetes-debug (pod logs, describe, health)    │  │
│  │  • MCP: github (commits, diffs, create revert PR)          │  │
│  │  • MCP: datadog/grafana (optional: query metrics)          │  │
│  └────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────┘
```

---

## Configuration

### `genie.toml`

```toml
skills_roots = ["./skills"]

[mcp]
[[mcp.servers]]
name = "github"
transport = "stdio"
command = "npx"
args = ["-y", "@anthropic/github-mcp-server"]
env = { GITHUB_TOKEN = "${GITHUB_TOKEN}" }

# Optional: Datadog MCP for metrics queries
# [[mcp.servers]]
# name = "datadog"
# transport = "streamable_http"
# server_url = "https://mcp.datadoghq.com/sse"
# env = { DD_API_KEY = "${DD_API_KEY}", DD_APP_KEY = "${DD_APP_KEY}" }
# include_tools = ["query_metrics", "query_logs", "list_monitors"]
```

---

## Interaction Flow

### Step 1 — Describe the incident

```
You: payment-service is throwing 500 errors in production.
     Namespace: prod, deployment: payment-service.
     Help me triage and find the root cause.
```

### Step 2 — Parallel triage (3 sub-agents)

The CodeOwner immediately spawns parallel investigations:

```
Genie: Starting parallel incident triage on payment-service in prod namespace...
```

```
# Sub-Agent A: Kubernetes status check
create_agent(
  goal="Run these commands and report findings:
        1. kubectl get pods -n prod -l app=payment-service -o wide
        2. kubectl describe deployment payment-service -n prod
        3. kubectl get events -n prod --sort-by='.lastTimestamp' | tail -20
        4. For any pod in CrashLoopBackOff or Error state, get logs:
           kubectl logs <pod-name> -n prod --tail=200
           kubectl logs <pod-name> -n prod --previous --tail=100
        Report: pod status, restart counts, recent events, error log excerpts.",
  task_type="tool_calling",
  tool_names=["execute_command"]
)

# Sub-Agent B: Recent deployment history
create_agent(
  goal="Investigate recent changes to payment-service:
        1. Run: git log --oneline -10 -- services/payment-service/
        2. Find the most recent deployment commit
        3. Run: git diff HEAD~1 -- services/payment-service/
        4. Summarize what changed in the last deployment
        Report: commit hash, author, timestamp, files changed, diff summary.",
  task_type="tool_calling",
  tool_names=["execute_command", "read_file", "search_content"]
)

# Sub-Agent C: Error pattern analysis
create_agent(
  goal="Analyze error patterns in the payment-service logs:
        1. Run: kubectl logs -n prod -l app=payment-service --since=30m | grep -i error | sort | uniq -c | sort -rn | head -20
        2. Identify the most frequent error message
        3. Search the codebase for that error string: search in services/payment-service/
        4. Read the file where the error originates and analyze the surrounding code
        Report: top error messages, frequency, source file and line, likely cause.",
  task_type="tool_calling",
  tool_names=["execute_command", "read_file", "search_content"]
)
```

### Step 3 — Orchestrator synthesizes root cause

The CodeOwner (orchestrator) does NOT delegate reasoning — it correlates the three reports:

```
Genie: ## Incident Triage Summary

       ### Kubernetes Status
       - 3/5 pods in CrashLoopBackOff (2 healthy, serving degraded traffic)
       - Restart count: 47 in last 30 minutes
       - OOMKilled events: 0 (not a memory issue)
       - Exit code 1 → application-level panic

       ### Recent Deployment
       - Last deploy: commit `a3f82b1` by @sarah, 45 minutes ago
       - Changed: `services/payment-service/pkg/repository/transaction.go`
       - Diff: Added new query joining `transactions` with `refunds` table
       
       ### Error Pattern
       - Top error (312 occurrences): 
         `pq: column "refund_status" does not exist`
       - Source: `pkg/repository/transaction.go:87`
       - The new query references `refunds.refund_status` but the migration
         for this column hasn't been applied to production yet.

       ### Root Cause
       **Commit `a3f82b1` added a query referencing `refunds.refund_status`,
       but the corresponding database migration was not included in the deploy.**

       ### Recommended Action
       **Option A:** Rollback to previous deployment (immediate, ~2 min)
       **Option B:** Apply the missing migration, then restart pods (~5 min)

       Which approach would you prefer?
```

### Step 4 — Execute the fix

```
You: Rollback to the previous deployment.
```

```
# Sub-Agent D: Execute rollback
create_agent(
  goal="Execute production rollback for payment-service:
        1. Run: kubectl rollout undo deployment/payment-service -n prod
        2. Wait for rollout: kubectl rollout status deployment/payment-service -n prod --timeout=120s
        3. Verify pods healthy: kubectl get pods -n prod -l app=payment-service
        4. Test endpoint: curl -s -o /dev/null -w '%{http_code}' https://api.internal/v1/payments/health
        5. Check error rate: kubectl logs -n prod -l app=payment-service --since=2m | grep -c 'error' || echo '0'
        Report: rollout status, pod health, health check result, error count.",
  task_type="tool_calling",
  tool_names=["execute_command"]
)
```

```
Genie: ✅ Rollback complete.

       - Deployment rolled back to revision 14 (commit `b7c91d4`)
       - All 5/5 pods Running and Ready
       - Health check: HTTP 200
       - Errors in last 2 minutes: 0

       ### Follow-Up Actions
       I recommend:
       1. Create a revert PR for commit `a3f82b1`
       2. Add the missing migration to the PR before re-deploying
       3. Add a CI check that validates migrations match code changes

       Shall I create the revert PR using the GitHub MCP tools?
```

### Step 5 — Post-incident cleanup

```
You: Yes, create the revert PR and add a note about the missing migration.
```

The orchestrator uses the GitHub MCP tools directly (simple task, no delegation needed):

```
Genie: Created PR #187: "revert: rollback transaction query (missing migration)"
       
       Added note in PR description:
       > This commit referenced `refunds.refund_status` without a corresponding
       > migration. The migration should be added and verified in staging before
       > re-deploying.
       
       Branch: revert-a3f82b1
       Status: Ready for review
```

---

## Key Concepts Demonstrated

| Concept | How It's Used |
|---------|---------------|
| **CodeOwner orchestrator** | Acts as incident commander — plans, correlates, decides |
| **Parallel sub-agents** | 3 simultaneous investigations (K8s, Git, Logs) cut triage time by 3x |
| **MCP: GitHub** | Creates revert PRs, queries commit history |
| **Skill: kubernetes-debug** | Structured pod debugging with health checks |
| **Shell access** | `kubectl`, `git`, `curl`, `grep` — all from one terminal |
| **Working memory** | Findings from Phase 1 inform reasoning in Phase 2 |
| **Delegation strategy** | Write-heavy (rollback, PR) → sub-agent. Reasoning (root cause) → orchestrator |

---

## Time Comparison

| Activity | Manual | With Genie |
|----------|--------|------------|
| Check pod status | 2 min | — |
| Pull and read logs | 5 min | — |
| Find recent deploy | 3 min | — |
| Read diff and correlate | 10 min | — |
| **Total triage** | **20 min** | **~3 min** (parallel) |
| Rollback deployment | 3 min | 2 min |
| Create revert PR | 5 min | 1 min |
| **Total resolution** | **~28 min** | **~6 min** |

---

## Emergency Playbook Integration

You can create a skill that encodes your team's incident playbooks:

```
skills/
└── incident-playbook/
    ├── SKILL.md          # Standard triage steps
    └── scripts/
        ├── triage.sh     # K8s health checks
        ├── rollback.sh   # Deployment rollback
        └── notify.sh     # Slack/PagerDuty updates
```

Then Genie can load and execute the playbook automatically:

```
You: Run the incident playbook for payment-service in prod.
```

This turns tribal knowledge into executable, repeatable incident response.
