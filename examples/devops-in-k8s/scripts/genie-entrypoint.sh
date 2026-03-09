#!/bin/sh
# genie-entrypoint.sh
# ──────────────────────────────────────────────────────────────────────
# Main container entrypoint. Installs CLI tools, copies the pre-generated
# kubeconfig from the shared volume, then drops privileges and runs genie.
#
# The genie container has IRSA credentials for AWS operations and kubectl
# access. API keys (OpenAI, Anthropic, etc.) are NOT in env vars — they
# are read from the resolved genie.toml on the shared volume.
# ──────────────────────────────────────────────────────────────────────
set -e

# ── 1. Core CLI tools ────────────────────────────────────────────────
#   Cloud:      aws-cli                     — AWS API operations via IRSA
#   Kubernetes: kubectl, helm               — cluster inspection, Helm release auditing
#   Data:       jq, yq                      — JSON/YAML parsing (AWS output + K8s manifests)
#   Networking: bind-tools (dig/nslookup),   — DNS debugging (#1 cause of intermittent failures)
#              openssl,                      — TLS cert inspection / expiry checks
#              nmap-ncat (nc)                — TCP port reachability testing
#   DB:         postgresql16-client (psql)   — direct DB diagnostics
#   SCM:        git                          — clone IaC repos, inspect drift
#   Shell:      curl, bash, su-exec          — HTTP client, scripting, privilege drop
apk add --no-cache \
  aws-cli \
  kubectl \
  helm \
  jq \
  yq \
  curl \
  bash \
  su-exec \
  git \
  bind-tools \
  openssl \
  nmap-ncat \
  postgresql16-client

# ── 2. Install trivy (container + K8s security scanner) ─────────────
# trivy is not in Alpine's default repos — install from GitHub releases.
if ! command -v trivy >/dev/null 2>&1; then
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  TRIVY_ARCH="Linux-64bit" ;;
    aarch64) TRIVY_ARCH="Linux-ARM64" ;;
    *)       TRIVY_ARCH="Linux-64bit" ;;
  esac
  TRIVY_VERSION="0.62.1"
  curl -sSL "https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_${TRIVY_ARCH}.tar.gz" \
    | tar xz -C /usr/local/bin trivy 2>/dev/null || echo "[entrypoint] trivy install skipped (network/arch)"
fi

# ── 3. Copy pre-generated kubeconfig from shared volume ──────────────
mkdir -p /home/stackgen/.kube
cp /shared-credentials/kubeconfig /home/stackgen/.kube/config
chown -R 65532:65532 /home/stackgen/.kube

# ── 4. Drop privileges and run genie ────────────────────────────────
exec su-exec 65532:65532 /usr/local/bin/genie \
  --config /shared-credentials/genie.toml \
  --log-level debug
