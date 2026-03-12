#!/bin/sh
# genie-entrypoint.sh
# ──────────────────────────────────────────────────────────────────────
# Main container entrypoint for the Marketing Intelligence Agent.
# The genie-beta image is minimal (no apk, no su-exec). Run genie
# directly — the pod security context handles user/group.
# ──────────────────────────────────────────────────────────────────────
set -e

export HOME=/home/stackgen
export TMPDIR=/tmp

mkdir -p /home/stackgen/work 2>/dev/null || true

exec /usr/local/bin/genie \
  --config /shared-credentials/genie.toml \
  --working-dir /home/stackgen/work \
  --log-level debug
