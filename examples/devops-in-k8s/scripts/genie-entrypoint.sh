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
#   SCM:        git, gh                      — clone IaC repos, inspect drift, GitHub API
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

# ── 5. Authenticate gh CLI with GITHUB_TOKEN from shared credentials ─
# The init container writes the token to a file on the shared volume so
# it never appears in env vars in this container.
#
# IMPORTANT: User 65532 has no /etc/passwd entry in Alpine, so HOME
# defaults to "/" and gh writes config to /.config/gh/ which sub-agents
# can't find. We explicitly set HOME so all processes share the same path.
export HOME=/home/stackgen

if [ -f /shared-credentials/github-token ] && command -v gh >/dev/null 2>&1; then
  mkdir -p /home/stackgen/.config/gh
  chown -R 65532:65532 /home/stackgen/.config
  su-exec 65532:65532 sh -c 'HOME=/home/stackgen cat /shared-credentials/github-token | gh auth login --with-token 2>/dev/null' \
    && echo "[entrypoint] gh CLI authenticated" \
    || echo "[entrypoint] gh auth skipped (invalid token?)"
fi

# ── 6. Drop privileges and run genie ────────────────────────────────
# HOME must be inherited so sub-agents (run_shell) can find gh config.
exec su-exec 65532:65532 /usr/local/bin/genie \
  --config /shared-credentials/genie.toml \
  --log-level debug
