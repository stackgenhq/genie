# Production Incident Triage — Kubernetes on AWS

> **Audience:** Developers troubleshooting production issues  
> **Scenario:** A developer on-call receives a PagerDuty alert — HTTP 500 error rate has spiked on a service. They need to identify root cause, correlate logs with recent deployments, and roll back or hot-fix in minutes.

## Agent Behavior Rules

> [!IMPORTANT]
> These rules govern how the agent should operate during incident triage. Follow them strictly.

### Tool Selection

- **NEVER use `create_agent` for simple kubectl or shell commands.** Use `run_shell` directly.
- Only spawn a sub-agent when the task genuinely requires multi-step reasoning or more than 5 sequential tool calls.
- If the user asks "what's the status of X", run the check commands directly — do NOT delegate to a sub-agent.

### Command Batching

- **Always batch related kubectl commands into a single shell invocation** using `&&` or `;`.
- Never issue the same kubectl command twice. If the output is already available, reuse it.

**Good — single call:**
```bash
kubectl get pods -n <namespace> -o wide && \
kubectl get svc -n <namespace> && \
kubectl get deployments -n <namespace> && \
kubectl get events -n <namespace> --sort-by='.lastTimestamp' | tail -20
```

**Bad — four separate calls:**
```bash
# Call 1
kubectl get pods -n <namespace> -o wide
# Call 2
kubectl get svc -n <namespace>
# Call 3
kubectl get deployments -n <namespace>
# Call 4
kubectl get events -n <namespace> --sort-by='.lastTimestamp' | tail -20
```

### Sub-Agent Guidelines

When a sub-agent IS appropriate (complex multi-step work):
- Give it a **compound goal** with all commands listed, not a vague description.
- Use `task_type: tool_calling` for kubectl/shell work.
- Set `max_tool_iterations` ≥ 15 and `max_llm_calls` ≥ 20.
- Prefer one sub-agent with a multi-part goal over multiple sub-agents with single commands.

### Avoid Loops

- If a command succeeds, **move on** — do not re-run it.
- If a command fails 3 times, report the failure and stop.
- Do not retry identical commands with identical arguments.

## Architecture

```
┌───────────────────────────────────────────────────────────────────┐
│                   CodeOwner (Incident Commander)                  │
│  Persona: Full developer with shell + file + MCP access          │
│  Memory:  Accumulates findings across investigation steps        │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  Quick Checks: DIRECT run_shell (no sub-agent)                   │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  kubectl get pods/svc/deploy, events, logs               │  │
│  │  → batched into ONE shell call, results reused            │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                   │
│  Complex Triage: Parallel sub-agents (ONLY when needed)          │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────┐  │
│  │  Sub-Agent A  │  │  Sub-Agent B  │  │     Sub-Agent C        │  │
│  │  K8s Deep     │  │  Git History  │  │   Log Analysis         │  │
│  │  Diagnostics  │  │  + Diff       │  │   + Error Patterns     │  │
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

## Prerequisites

- `kubectl` configured with the target EKS cluster context
- AWS CLI authenticated with sufficient IAM permissions
- Access to monitoring (CloudWatch, Prometheus/Grafana, or Datadog)

## Quick Status Check (Direct — No Sub-Agent)

When the user asks about the status of a service or namespace, run this **single** compound command directly via `run_shell`:

```bash
NS="<namespace>"
echo "=== PODS ===" && kubectl get pods -n $NS -o wide && \
echo "=== SERVICES ===" && kubectl get svc -n $NS && \
echo "=== DEPLOYMENTS ===" && kubectl get deployments -n $NS && \
echo "=== RECENT EVENTS ===" && kubectl get events -n $NS --sort-by='.lastTimestamp' | tail -20
```

Parse the output and respond immediately. Do NOT spawn a sub-agent for this.

## Triage Workflow

### 1. Confirm cluster connectivity

```bash
kubectl config current-context && kubectl cluster-info && \
aws eks describe-cluster --name <cluster-name> --query 'cluster.status'
```

### 2. Check node health

```bash
kubectl get nodes -o wide
kubectl describe node <node-name>         # look for Conditions, Taints, and capacity pressure
aws ec2 describe-instance-status --filters Name=tag:eks:cluster-name,Values=<cluster-name>
```

- **NotReady nodes**: check kubelet logs, ENI limits, or instance health in EC2 console.
- **MemoryPressure / DiskPressure**: cordon the node and investigate resource hogs.

### 3. Inspect workloads

```bash
kubectl get pods -n <namespace> --field-selector=status.phase!=Running && \
kubectl describe pod <pod-name> -n <namespace> && \
kubectl logs <pod-name> -n <namespace> --tail=200 --previous   # crash-looped containers
```

| Symptom | Likely cause | Next step |
|---------|-------------|-----------|
| `CrashLoopBackOff` | Application error or missing config | Check logs and ConfigMaps/Secrets |
| `ImagePullBackOff` | Bad image tag or ECR auth | Verify image URI and IAM/ECR token |
| `Pending` | Insufficient resources or node affinity | Check events, resource requests, node capacity |
| `OOMKilled` | Memory limit too low | Increase limits or fix memory leak |

### 4. Check recent deployments

```bash
kubectl rollout history deployment/<name> -n <namespace>
kubectl rollout undo deployment/<name> -n <namespace>         # if a bad deploy is suspected
```

### 5. Inspect AWS-level issues

```bash
aws cloudwatch get-metric-statistics --namespace AWS/EKS ...  # API server latency
aws elbv2 describe-target-health --target-group-arn <arn>      # ALB/NLB target health
aws logs filter-log-events --log-group-name /aws/eks/<cluster>/cluster --filter-pattern "ERROR"
```

Key failure surfaces:
- **ENI exhaustion**: pods stuck `Pending`, `FailedCreatePodSandBox` → check `aws-node` (VPC CNI) logs.
- **IAM / IRSA**: `AccessDenied` in pod logs → verify ServiceAccount annotation and IAM trust policy.
- **EBS CSI**: `FailedAttachVolume` → check PV/PVC status and `aws ec2 describe-volumes`.

### 6. Networking

```bash
kubectl get svc,ingress -n <namespace> && \
kubectl exec <pod> -n <namespace> -- curl -sS http://<service>:<port>/healthz
```

- Verify Security Groups allow required traffic.
- Confirm CoreDNS is healthy: `kubectl -n kube-system get pods -l k8s-app=kube-dns`.

## Example Interaction

```
You: payment-service is throwing 500 errors in production.
     Namespace: prod, deployment: payment-service.
     Help me triage and find the root cause.
```

Genie runs a quick status check directly, then if deeper investigation is needed, spawns parallel sub-agents for git history and log analysis:

```
Genie: ## Incident Triage Summary

       ### Root Cause
       Commit `a3f82b1` added a query referencing `refunds.refund_status`,
       but the corresponding database migration was not included in the deploy.

       ### Recommended Action
       **Option A:** Rollback to previous deployment (immediate, ~2 min)
       **Option B:** Apply the missing migration, then restart pods (~5 min)
```

After choosing rollback, a sub-agent executes `kubectl rollout undo`, verifies pod health, and optionally creates a revert PR via GitHub MCP.

## Escalation Checklist

- [ ] Incident Slack channel created with timeline
- [ ] On-call engineer paged (PagerDuty / Opsgenie)
- [ ] Blast radius assessed (affected services, users, regions)
- [ ] Rollback performed if applicable
- [ ] Root cause documented in post-mortem

## Emergency Playbook Integration

```
skills/
└── incident-playbook/
    ├── SKILL.md          # Standard triage steps
    └── scripts/
        ├── triage.sh     # K8s health checks
        ├── rollback.sh   # Deployment rollback
        └── notify.sh     # Slack/PagerDuty updates
```

```
You: Run the incident playbook for payment-service in prod.
```

## Time Comparison

| Activity | Manual | With Genie |
|----------|--------|------------|
| Check pod status + logs + git diff | 20 min | ~3 min (parallel) |
| Rollback deployment | 3 min | 2 min |
| Create revert PR | 5 min | 1 min |
| **Total resolution** | **~28 min** | **~6 min** |

## Code Style

- Shell scripts use `set -euo pipefail`
- Wrap `kubectl` commands in retry loops for flaky API server connections
- Use `--output json` and `jq` for automation; human-readable output for manual triage
