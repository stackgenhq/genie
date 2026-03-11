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
#   SCM:        git, gh                      — clone IaC repos, inspect drift (gh via gh_cli tool)
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

# ── 3. Install gh (GitHub CLI) ──────────────────────────────────────
# gh is not in Alpine repos — install from GitHub releases.
# The binary must be on PATH for the gh_cli agent tool to activate.
# Authentication is handled at runtime by the ghcli tool provider
# (injects GH_TOKEN per-subprocess), so no `gh auth login` needed here.
if ! command -v gh >/dev/null 2>&1; then
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  GH_ARCH="linux_amd64" ;;
    aarch64) GH_ARCH="linux_arm64" ;;
    *)       GH_ARCH="linux_amd64" ;;
  esac
  GH_VERSION="2.87.3"
  curl -sSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_${GH_ARCH}.tar.gz" \
    | tar xz --strip-components=2 -C /usr/local/bin "gh_${GH_VERSION}_${GH_ARCH}/bin/gh" 2>/dev/null \
    || echo "[entrypoint] gh install skipped (network/arch)"
fi

# ── 4. Copy pre-generated kubeconfig from shared volume ──────────────
mkdir -p /home/stackgen/.kube
cp /shared-credentials/kubeconfig /home/stackgen/.kube/config
chown -R 65532:65532 /home/stackgen/.kube

# ── 5. Drop privileges and run genie ────────────────────────────────
# HOME must be set explicitly: user 65532 has no /etc/passwd entry in
# Alpine, so HOME defaults to "/" without this export.
export HOME=/home/stackgen
# TMPDIR must point to a writable directory. Without this, Go's
# os.MkdirTemp("", ...) defaults to os.TempDir() which returns "/"
# for users without a passwd entry — and "/" is not writable in
# containers with a read-only root filesystem or non-root users.
# This caused the run_shell tool's circuit breaker to trip on every
# AWS CLI command (os.MkdirTemp used by the code executor to write
# intermediate script files).
export TMPDIR=/tmp
# Ensure all user directories are writable by the genie user (gh cli, aws cli caches, etc.)
mkdir -p /home/stackgen/.aws
mkdir -p /home/stackgen/work
chown -R 65532:65532 /home/stackgen

exec su-exec 65532:65532 env HOME=/home/stackgen TMPDIR=/tmp /usr/local/bin/genie \
  --config /shared-credentials/genie.toml \
  --working-dir /home/stackgen/work \
  --log-level debug
