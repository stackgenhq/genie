#!/bin/bash
# Check that all Go files have the correct SPDX headers based on their directories.

set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
cd "$REPO_ROOT"

echo "Checking license headers..."
FAIL=0

# Iterate over all go files, excluding vendor, hidden folders, out directories, and generated fakes
for f in $(find . -name "*.go" -not -path "*/vendor/*" -not -path "*/.*" -not -path "*/*fakes*" -type f | sed 's|^\./||'); do
    if [[ "$f" == pkg/reactree/* ]] || [[ "$f" == pkg/halguard/* ]] || [[ "$f" == pkg/orchestrator/* ]] || [[ "$f" == pkg/semanticrouter/* ]]; then
        if ! grep -q "SPDX-License-Identifier: BUSL-1.1" "$f"; then
            echo "Missing BSL header: $f"
            FAIL=1
        fi
    else
        if ! grep -q "SPDX-License-Identifier: Apache-2.0" "$f"; then
            echo "Missing Apache header: $f"
            FAIL=1
        fi
    fi
done

if [ $FAIL -ne 0 ]; then
    echo "Missing or incorrect SPDX license headers found."
    echo "Please add them manually or use a script to add them."
    exit 1
fi

echo "All files have correct license headers."
exit 0
