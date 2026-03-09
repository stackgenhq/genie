#!/bin/sh
# credential-refresh.sh
# ──────────────────────────────────────────────────────────────────────
# Runs as a sidecar alongside the main genie container. Periodically
# refreshes the kubeconfig so the IRSA token (24h expiry) stays valid.
# This container has IRSA credentials but is NOT user-accessible.
# ──────────────────────────────────────────────────────────────────────

REFRESH_INTERVAL_SECONDS=43200  # 12 hours

echo "[credential-refresh] Starting periodic kubeconfig refresh (every ${REFRESH_INTERVAL_SECONDS}s)."

while true; do
  sleep "$REFRESH_INTERVAL_SECONDS"
  echo "[credential-refresh] Refreshing kubeconfig..."
  aws eks update-kubeconfig \
    --region "$AWS_REGION" \
    --name "$EKS_CLUSTER_NAME" \
    --kubeconfig /shared-credentials/kubeconfig \
    --cli-connect-timeout 10 || echo "[credential-refresh] WARNING: refresh failed, will retry."
  chmod 0600 /shared-credentials/kubeconfig
done
