#!/bin/sh
# credential-bootstrap.sh
# ──────────────────────────────────────────────────────────────────────
# Runs as an init container BEFORE the main genie container starts.
# Has access to all secrets and the IRSA token. Generates kubeconfig
# and resolves the remaining runtime placeholders in genie.toml.
#
# Infrastructure values (Qdrant host, AWS region, Secrets Manager name)
# are already resolved by Terraform's templatefile() when the ConfigMap
# was created. Only POSTGRES_DSN remains as a runtime placeholder
# because the password lives in a Kubernetes Secret, not in Terraform.
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

# 2. Resolve genie.toml: only POSTGRES_DSN needs runtime substitution.
#    All other infrastructure values (Qdrant endpoints, Secrets Manager
#    name, AWS region) were pre-filled by Terraform's templatefile().
cp /app-config/genie.toml /tmp/genie.toml.tpl
envsubst '$POSTGRES_DSN' < /tmp/genie.toml.tpl > /shared-credentials/genie.toml
chmod 0640 /shared-credentials/genie.toml

# 3. Copy AGENTS.md (not sensitive, but keeps mounts clean)
cp /app-config/AGENTS.md /shared-credentials/AGENTS.md
chmod 0644 /shared-credentials/AGENTS.md

# 4. Ensure the genie user (65532) can read everything
chown -R 65532:65532 /shared-credentials

echo "[credential-bootstrap] Credentials bootstrapped successfully."
