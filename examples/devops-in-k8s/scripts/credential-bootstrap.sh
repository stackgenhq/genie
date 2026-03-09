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

# 0. Install envsubst (from gettext) and jq — not bundled in amazon/aws-cli image
echo "[credential-bootstrap] Installing envsubst and jq..."
yum install -y -q gettext jq >/dev/null 2>&1

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
envsubst '$POSTGRES_DSN $SECRETS_MANAGER_ARN $SECRETS_MANAGER_NAME $AWS_REGION' < /tmp/genie.toml.tpl > /shared-credentials/genie.toml
chmod 0640 /shared-credentials/genie.toml

# 3. Copy AGENTS.md (not sensitive, but keeps mounts clean)
cp /app-config/AGENTS.md /shared-credentials/AGENTS.md
chmod 0644 /shared-credentials/AGENTS.md

# 4. Write GITHUB_TOKEN for gh CLI authentication in the genie container.
#    Written to a separate file (not env var) to maintain credential isolation.
GITHUB_TOKEN=$(aws secretsmanager get-secret-value --secret-id "$SECRETS_MANAGER_ARN" --region "$AWS_REGION" --query SecretString --output text | jq -r '.GITHUB_TOKEN // empty')
if [ -n "${GITHUB_TOKEN:-}" ]; then
  printf '%s' "$GITHUB_TOKEN" > /shared-credentials/github-token
  chmod 0640 /shared-credentials/github-token
fi

# 5. Ensure the genie user (65532) can read everything
chown -R 65532:65532 /shared-credentials

echo "[credential-bootstrap] Credentials bootstrapped successfully."
