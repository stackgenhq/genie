#!/bin/sh
# credential-bootstrap.sh
# ──────────────────────────────────────────────────────────────────────
# Runs as an init container BEFORE the main genie container starts.
# Has access to all secrets and the IRSA token. Generates kubeconfig
# and resolves genie.toml with real credential values, writing both
# to /shared-credentials so the genie container (which has NO secrets
# in its environment) can read them.
# ──────────────────────────────────────────────────────────────────────
set -e

# 0. Install envsubst (from gettext) — not bundled in amazon/aws-cli image
echo "[credential-bootstrap] Installing envsubst..."
yum install -y -q gettext >/dev/null 2>&1

# 1. Generate kubeconfig using IRSA credentials
aws eks update-kubeconfig \
  --region "$AWS_REGION" \
  --name "$EKS_CLUSTER_NAME" \
  --kubeconfig /shared-credentials/kubeconfig \
  --cli-connect-timeout 10

chmod 0640 /shared-credentials/kubeconfig

# 2. Resolve genie.toml: substitute ${VAR} placeholders with actual
#    environment variable values so the genie binary can read credentials
#    from the file instead of env vars.
cp /app-config/genie.toml /tmp/genie.toml.tpl
envsubst < /tmp/genie.toml.tpl > /shared-credentials/genie.toml
chmod 0640 /shared-credentials/genie.toml

# 3. Copy AGENTS.md (not sensitive, but keeps mounts clean)
cp /app-config/AGENTS.md /shared-credentials/AGENTS.md
chmod 0644 /shared-credentials/AGENTS.md

# 4. Ensure the genie user (65532) can read everything
chown -R 65532:65532 /shared-credentials

echo "[credential-bootstrap] Credentials bootstrapped successfully."
