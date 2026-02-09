#!/bin/bash
# Prepare branch for pull request

set -e

TARGET_BRANCH="${1:-main}"
OUTPUT_DIR="${OUTPUT_DIR:-./output}"
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

if [ "$CURRENT_BRANCH" = "$TARGET_BRANCH" ]; then
    echo "Error: You are on $TARGET_BRANCH. Switch to your feature branch first."
    exit 1
fi

echo "Preparing branch for pull request"
echo "Current branch: $CURRENT_BRANCH"
echo "Target branch: $TARGET_BRANCH"
echo ""

# Fetch latest changes
echo "Fetching latest changes..."
git fetch origin

# Check if target branch exists
if ! git rev-parse --verify "origin/$TARGET_BRANCH" >/dev/null 2>&1; then
    echo "Error: Target branch origin/$TARGET_BRANCH does not exist"
    exit 1
fi

# Rebase on target branch
echo "Rebasing on $TARGET_BRANCH..."
if ! git rebase "origin/$TARGET_BRANCH"; then
    echo "Error: Rebase failed. Please resolve conflicts and run 'git rebase --continue'"
    exit 1
fi

# Generate PR description from commits
echo "Generating PR description..."
mkdir -p "$OUTPUT_DIR"

cat > "$OUTPUT_DIR/pr_description.md" <<EOF
## Description

<!-- Describe your changes in detail -->

## Changes Made

$(git log --pretty=format:"- %s" "origin/$TARGET_BRANCH..HEAD")

## Type of Change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update

## Testing

<!-- Describe the tests you ran -->

- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Checklist

- [ ] My code follows the project's style guidelines
- [ ] I have performed a self-review of my own code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] I have made corresponding changes to the documentation
- [ ] My changes generate no new warnings
- [ ] I have added tests that prove my fix is effective or that my feature works
- [ ] New and existing unit tests pass locally with my changes

## Related Issues

<!-- Link related issues here -->
Closes #

---
Branch: $CURRENT_BRANCH
Target: $TARGET_BRANCH
Commits: $(git rev-list --count "origin/$TARGET_BRANCH..HEAD")
EOF

echo ""
echo "✓ Branch prepared for pull request"
echo "✓ PR description generated: $OUTPUT_DIR/pr_description.md"
echo ""
echo "Next steps:"
echo "1. Review the generated PR description"
echo "2. Push your branch: git push -u origin $CURRENT_BRANCH"
echo "3. Create pull request on GitHub/GitLab"

cat "$OUTPUT_DIR/pr_description.md"
