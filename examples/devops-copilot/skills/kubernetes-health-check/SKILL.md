---
name: kubernetes-health-check
description: Comprehensive Kubernetes cluster health check covering nodes, pods, resource utilization, and common failure patterns
---

# Kubernetes Health Check

Perform a full cluster health check including node status, pod health, resource utilization, pending pods, failing deployments, and common issues.

## When to Use This Skill

Use this skill when:
- A general cluster health assessment is needed
- Resource pressure or capacity issues are suspected
- Pods are failing or stuck in pending/crash states
- Pre-deployment sanity checks are needed

## Prerequisites

- `kubectl` configured with cluster access
- Either `kubectl top` working (requires metrics-server) or Prometheus access for resource metrics

## Workflow

### Step 1: Cluster Overview

```bash
echo "=== CLUSTER INFO ===" && \
kubectl cluster-info && \
echo "" && \
echo "=== NODE STATUS ===" && \
kubectl get nodes -o wide && \
echo "" && \
echo "=== NODE CONDITIONS (non-Ready) ===" && \
kubectl get nodes -o json | jq '.items[] | select(.status.conditions[] | select(.type=="Ready" and .status!="True")) | .metadata.name'
```

### Step 2: Node Resource Utilization

```bash
echo "=== NODE RESOURCE USAGE ===" && \
kubectl top nodes 2>/dev/null || echo "(metrics-server not available, skipping)" && \
echo "" && \
echo "=== NODE PRESSURE CONDITIONS ===" && \
kubectl describe nodes | grep -A5 "Conditions:" | grep -E "True|MemoryPressure|DiskPressure|PIDPressure"
```

### Step 3: Pod Health Across Namespaces

```bash
echo "=== PODS NOT RUNNING ===" && \
kubectl get pods --all-namespaces --field-selector 'status.phase!=Running,status.phase!=Succeeded' -o wide && \
echo "" && \
echo "=== CRASH-LOOPING PODS ===" && \
kubectl get pods --all-namespaces -o json | jq '.items[] | select(.status.containerStatuses[]?.restartCount > 5) | {namespace: .metadata.namespace, name: .metadata.name, restarts: .status.containerStatuses[].restartCount}' && \
echo "" && \
echo "=== PENDING PODS ===" && \
kubectl get pods --all-namespaces --field-selector 'status.phase=Pending' -o wide
```

### Step 4: Recent Events

```bash
echo "=== WARNING EVENTS (last 1 hour) ===" && \
kubectl get events --all-namespaces --sort-by='.lastTimestamp' --field-selector type=Warning | tail -30 && \
echo "" && \
echo "=== FAILED EVENTS ===" && \
kubectl get events --all-namespaces --field-selector reason=Failed,reason=FailedScheduling,reason=FailedMount | tail -20
```

### Step 5: Resource Quotas and Limits

```bash
echo "=== RESOURCE QUOTAS ===" && \
kubectl get resourcequotas --all-namespaces -o wide && \
echo "" && \
echo "=== LIMIT RANGES ===" && \
kubectl get limitranges --all-namespaces
```

### Step 6: Deployment Health

```bash
echo "=== DEPLOYMENTS NOT FULLY AVAILABLE ===" && \
kubectl get deployments --all-namespaces -o json | jq '.items[] | select(.status.availableReplicas < .status.replicas or .status.availableReplicas == null) | {namespace: .metadata.namespace, name: .metadata.name, desired: .status.replicas, available: .status.availableReplicas}'
```

## Output

Present a health report with:
- **Cluster Summary**: node count, K8s version, overall status
- **Node Health**: table of nodes with CPU/memory utilization
- **Problem Pods**: list of failing, pending, or crash-looping pods
- **Recent Events**: warning events and failures
- **Resource Pressure**: quotas near limits, nodes under pressure
- **Recommendations**: actions to address identified issues
