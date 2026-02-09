#!/bin/bash
# Create a feature branch with proper naming convention

set -e

FEATURE_NAME="$1"
BASE_BRANCH="${2:-main}"
OUTPUT_DIR="${OUTPUT_DIR:-./output}"

if [ -z "$FEATURE_NAME" ]; then
    echo "Usage: $0 <feature-name> [base-branch]"
    echo "Example: $0 user-authentication main"
    exit 1
fi

# Sanitize feature name (replace spaces with hyphens, lowercase)
FEATURE_NAME=$(echo "$FEATURE_NAME" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')

# Determine branch type
if [[ "$FEATURE_NAME" == bug-* ]] || [[ "$FEATURE_NAME" == fix-* ]]; then
    BRANCH_TYPE="bugfix"
elif [[ "$FEATURE_NAME" == hotfix-* ]]; then
    BRANCH_TYPE="hotfix"
elif [[ "$FEATURE_NAME" == docs-* ]]; then
    BRANCH_TYPE="docs"
elif [[ "$FEATURE_NAME" == refactor-* ]]; then
    BRANCH_TYPE="refactor"
else
    BRANCH_TYPE="feature"
fi

BRANCH_NAME="$BRANCH_TYPE/$FEATURE_NAME"

echo "Creating branch: $BRANCH_NAME"
echo "Base branch: $BASE_BRANCH"

# Ensure we're on base branch and up to date
git checkout "$BASE_BRANCH"
git pull origin "$BASE_BRANCH"

# Create and checkout new branch
git checkout -b "$BRANCH_NAME"

mkdir -p "$OUTPUT_DIR"
cat > "$OUTPUT_DIR/branch_info.txt" <<EOF
Branch Created Successfully
===========================

Branch Name: $BRANCH_NAME
Branch Type: $BRANCH_TYPE
Base Branch: $BASE_BRANCH
Created: $(date)

Next Steps:
1. Make your changes
2. Commit with descriptive messages
3. Push to remote: git push -u origin $BRANCH_NAME
4. Create pull request when ready

Commit Message Template:
$BRANCH_TYPE($FEATURE_NAME): <short description>

<detailed description>

Closes #<issue-number>
EOF

cat "$OUTPUT_DIR/branch_info.txt"
echo ""
echo "✓ Branch created: $BRANCH_NAME"
echo "✓ You are now on the new branch"
