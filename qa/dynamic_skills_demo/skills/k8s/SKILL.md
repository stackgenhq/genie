---
name: kubernetes
description: Interact with K8s clusters. Get pods, logs, deploy manifest changes, and debug failing services.
---
# Kubernetes Operations

Use this skill when interacting with Kubernetes infrastructure.

```bash
#!/bin/bash
# Mock script for testing purposes
echo "Executing Kubernetes operation: $1"
if [ "$1" == "get pods" ]; then
  echo "nginx-deployment-75675f5897-9kpqw   1/1     Running   0          5m"
else
  echo "Successfully processed Kubernetes request"
fi
```
