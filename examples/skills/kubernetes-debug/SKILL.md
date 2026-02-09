---
name: kubernetes-debug
description: Debug Kubernetes pods, services, and deployments with comprehensive diagnostic tools
---

# Kubernetes Debug

A comprehensive skill for debugging Kubernetes resources including pods, services, deployments, and networking issues.

## When to Use This Skill

Use this skill when you need to:
- Investigate pod failures or crashes
- Debug service connectivity issues
- Analyze resource usage and limits
- Inspect deployment rollout status
- Troubleshoot networking problems
- Examine pod logs and events

## Available Commands

### Get Pod Logs

Retrieve logs from a specific pod with optional filtering and tail options.

```bash
python3 scripts/get_logs.py <namespace> <pod-name> [--tail=100] [--previous] [--container=name]
```

**Parameters:**
- `namespace`: Kubernetes namespace
- `pod-name`: Name of the pod
- `--tail`: Number of lines to show (default: 100)
- `--previous`: Show logs from previous container instance
- `--container`: Specific container name (for multi-container pods)

### Describe Resource

Get detailed information about a Kubernetes resource.

```bash
python3 scripts/describe_resource.py <namespace> <resource-type> <resource-name>
```

**Parameters:**
- `namespace`: Kubernetes namespace
- `resource-type`: Type of resource (pod, service, deployment, etc.)
- `resource-name`: Name of the resource

### Check Pod Health

Analyze pod health including readiness, liveness probes, and resource usage.

```bash
python3 scripts/check_health.py <namespace> <pod-name>
```

### Port Forward

Set up port forwarding to access a pod or service locally.

```bash
python3 scripts/port_forward.py <namespace> <resource-type> <resource-name> <local-port>:<remote-port>
```

## Environment Variables

The scripts expect `kubectl` to be configured and available in PATH. Ensure your kubeconfig is set up correctly:

```bash
export KUBECONFIG=~/.kube/config
```

## Dependencies

- Python 3.8+
- kubectl CLI tool
- Valid kubeconfig with cluster access

## Output

All scripts write their output to the `$OUTPUT_DIR` directory:
- `logs.txt`: Pod logs
- `describe.yaml`: Resource description
- `health_report.json`: Health check results
- `port_forward.log`: Port forwarding status

## Troubleshooting

### kubectl not found
Ensure kubectl is installed and in your PATH:
```bash
which kubectl
```

### Permission denied
Verify your kubeconfig has the necessary RBAC permissions:
```bash
kubectl auth can-i get pods --namespace=<namespace>
```

### Connection timeout
Check cluster connectivity:
```bash
kubectl cluster-info
```
