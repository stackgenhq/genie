## 🏗️ Phase 1: Monitoring & Alerting Setup

To effectively monitor CPU, you need a metrics pipeline (Prometheus + Grafana is the industry standard).

### 1. The Metrics Query (PromQL)

Use this query to identify Pods exceeding their CPU request or limit:

* **Usage vs. Request:** `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate) by (pod) / kube_pod_container_resource_requests{resource="cpu"} > 0.8`
* **Usage vs. Limit:** `sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate) by (pod) / kube_pod_container_resource_limits{resource="cpu"} > 0.9`

### 2. Alert Definition

Set up an alert in Alertmanager or your monitoring tool:

* **Critical:** CPU > 90% of limit for 5 minutes.
* **Warning:** CPU > 75% of limit for 15 minutes.

---

## 🛠️ Phase 2: Triage & Investigation

When an alert fires, follow these steps to diagnose the root cause:

### Step 1: Identify the Impacted Pods

Use `kubectl` to see real-time usage across the namespace:

```bash
kubectl top pods -n <namespace> --sort-by=cpu

```

### Step 2: Check for Throttling

Even if CPU isn't at 100%, "CPU Throttling" can kill performance if you hit your limit.

```bash
# Look for 'nr_throttled' in the container's cpu.stat (via exec or metrics)
kubectl exec <pod-name> -n <namespace> -- cat /sys/fs/cgroup/cpu/cpu.stat

```

### Step 3: Inspect Pod Events

Check if the high CPU is causing Pod restarts or liveness probe failures.

```bash
kubectl describe pod <pod-name> -n <namespace>

```

---

## ⚡ Phase 3: Mitigation Strategies

Depending on what you find, choose one of the following paths:

### Option A: Horizontal Scaling (Add Pods)

If the load is legitimate traffic, scale the deployment to distribute the work.

```bash
kubectl scale deployment <deployment-name> --replicas=<new-number> -n <namespace>

```

*Note: Ensure your HPA (Horizontal Pod Autoscaler) is configured for future automation.*

### Option B: Vertical Scaling (Increase Limits)

If a single process needs more "oomph" and can't be parallelized:

1. Update the `resources.limits.cpu` and `resources.requests.cpu` in your Deployment YAML.
2. Apply the change: `kubectl apply -f deployment.yaml`.

### Option C: Identify "Noisy Neighbors"

Sometimes high CPU on a Service is actually caused by the **Node** being over-utilized.

```bash
kubectl top nodes

```

If a node is at 90%+, consider draining the node to move pods to a quieter host.

---

## 📝 Phase 4: Root Cause Analysis (RCA)

Once the service is stable, investigate **why** it happened:

| Possible Cause | Investigation Tool |
| --- | --- |
| **Traffic Spike** | Check Load Balancer/Ingress logs for request volume. |
| **Resource Leak** | Use a profiler (e.g., pprof for Go, JVisualVM for Java). |
| **Inefficient Code** | Review recent commits for expensive loops or regex. |
| **Garbage Collection** | Check if the language runtime is "STW" (Stop The World) during GC. |

---
