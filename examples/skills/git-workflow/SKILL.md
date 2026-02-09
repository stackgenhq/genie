---
name: git-workflow
description: Automate common Git workflows including feature branches, rebasing, and pull request preparation
---

# Git Workflow

Automates common Git workflows and enforces best practices for branch management, commits, and pull requests.

## When to Use This Skill

Use this skill when you need to:
- Create feature branches following naming conventions
- Prepare branches for pull requests
- Rebase feature branches on main/master
- Clean up merged branches
- Squash commits before merging
- Generate changelog from commits

## Available Commands

### Create Feature Branch

Creates a new feature branch with proper naming convention.

```bash
bash scripts/create_feature.sh <feature-name> [base-branch]
```

**Example:**
```bash
bash scripts/create_feature.sh user-authentication main
# Creates: feature/user-authentication
```

### Prepare for Pull Request

Prepares your branch for a pull request by rebasing, squashing, and running checks.

```bash
bash scripts/prepare_pr.sh [target-branch]
```

This will:
1. Fetch latest changes
2. Rebase on target branch (default: main)
3. Run pre-commit checks
4. Generate PR description from commits

### Rebase on Main

Safely rebase your feature branch on the latest main/master.

```bash
bash scripts/rebase_main.sh [main-branch-name]
```

### Clean Merged Branches

Remove local branches that have been merged.

```bash
bash scripts/clean_branches.sh [--dry-run]
```

### Generate Changelog

Generate a changelog from commit messages.

```bash
bash scripts/changelog.sh <from-ref> <to-ref>
```

**Example:**
```bash
bash scripts/changelog.sh v1.0.0 HEAD
```

## Branch Naming Conventions

The skill enforces these branch naming patterns:
- `feature/<description>` - New features
- `bugfix/<description>` - Bug fixes
- `hotfix/<description>` - Urgent production fixes
- `refactor/<description>` - Code refactoring
- `docs/<description>` - Documentation changes

## Commit Message Format

Recommended commit message format (Conventional Commits):
```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:** feat, fix, docs, style, refactor, test, chore

**Example:**
```
feat(auth): add OAuth2 authentication

Implement OAuth2 authentication flow with Google and GitHub providers.
Includes token refresh and session management.

Closes #123
```

## Output

Scripts write to `$OUTPUT_DIR`:
- `pr_description.md`: Generated pull request description
- `changelog.md`: Generated changelog
- `rebase_log.txt`: Rebase operation log
- `branch_cleanup.txt`: List of cleaned branches

## Best Practices

### Before Creating PR
1. Rebase on target branch
2. Squash fixup commits
3. Ensure tests pass
4. Update documentation
5. Write clear PR description

### Commit Guidelines
- Keep commits atomic and focused
- Write descriptive commit messages
- Reference issue numbers
- Use conventional commit format
- Don't commit sensitive data

### Branch Management
- Keep branches short-lived
- Delete merged branches
- Rebase frequently
- Avoid merge commits in feature branches

## Dependencies

- Git 2.23+
- bash 4.0+

## Troubleshooting

### Rebase conflicts
If rebase fails with conflicts:
```bash
# Fix conflicts in files
git add <resolved-files>
git rebase --continue

# Or abort and try manually
git rebase --abort
```

### Detached HEAD
If you end up in detached HEAD state:
```bash
git checkout <your-branch-name>
```
