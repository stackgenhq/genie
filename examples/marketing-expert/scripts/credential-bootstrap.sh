#!/bin/sh
# credential-bootstrap.sh
# ──────────────────────────────────────────────────────────────────────
# Runs as an init container BEFORE the main genie container starts.
# Resolves the remaining runtime placeholders in genie.toml.
#
# Infrastructure values (Qdrant host, AWS region, Secrets Manager name)
# are already resolved by Terraform's templatefile() when the ConfigMap
# was created. Only MARKETING_POSTGRES_DSN remains as a runtime
# placeholder because the password lives in a Kubernetes Secret.
#
# Unlike devops-copilot, this does NOT generate a kubeconfig because
# the marketing agent has no need for kubectl or AWS CLI access.
#
# The existing postgres-credentials secret (from devops-copilot) provides:
#   POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB
# We build a DSN pointing to the genie_marketing database instead.
# ──────────────────────────────────────────────────────────────────────
set -e

# 0. Install envsubst (from gettext) — not bundled in amazon/aws-cli image
echo "[credential-bootstrap] Installing envsubst..."
yum install -y -q gettext >/dev/null 2>&1

# 1. Build the marketing-specific DSN from existing PostgreSQL credentials.
#    The K8s Job (create-marketing-db) has already created the genie_marketing
#    database. We reuse POSTGRES_USER and POSTGRES_PASSWORD from the existing
#    postgres-credentials secret, but point to the marketing database.
export MARKETING_POSTGRES_DSN="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres.${NAMESPACE:-genie}.svc.cluster.local:5432/genie_marketing?sslmode=disable"

# 2. Resolve genie.toml: substitute MARKETING_POSTGRES_DSN.
cp /app-config/genie.toml /tmp/genie.toml.tpl
envsubst '$MARKETING_POSTGRES_DSN' < /tmp/genie.toml.tpl > /shared-credentials/genie.toml
chmod 0640 /shared-credentials/genie.toml

# 3. Copy AGENTS.md (not sensitive, but keeps mounts clean)
cp /app-config/AGENTS.md /shared-credentials/AGENTS.md
chmod 0644 /shared-credentials/AGENTS.md

# 4. Ensure the genie user (65532) can read everything
chown -R 65532:65532 /shared-credentials

echo "[credential-bootstrap] Credentials bootstrapped successfully."
