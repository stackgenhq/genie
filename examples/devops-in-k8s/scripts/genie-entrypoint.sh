#!/bin/sh
# genie-entrypoint.sh
# ──────────────────────────────────────────────────────────────────────
# Main container entrypoint. Installs CLI tools, copies the pre-generated
# kubeconfig from the shared volume, then drops privileges and runs genie.
#
# SECURITY: This container has ZERO secret env vars and NO IRSA token
# mount. All credentials are read from /shared-credentials (written by
# the init container).
# ──────────────────────────────────────────────────────────────────────
set -e

# Install CLI tools (runs as root)
apk add --no-cache kubectl jq curl bash su-exec

# Copy pre-generated kubeconfig from shared volume
mkdir -p /home/stackgen/.kube
cp /shared-credentials/kubeconfig /home/stackgen/.kube/config
chown -R 65532:65532 /home/stackgen/.kube
chown -R 65532:65532 /data

# Drop privileges and run genie
exec su-exec 65532:65532 /usr/local/bin/genie \
  --config /shared-credentials/genie.toml \
  --log-level debug
