# CI/CD Runbook

Reference guide for ArgoCD, GitHub Actions, and pipeline management.

---

## ArgoCD

### Application Management

```bash
# List all applications
argocd app list --output wide

# Get application status
argocd app get <app-name> --output json | jq '{status: .status.sync.status, health: .status.health.status, revision: .status.sync.revision}'

# Show differences between desired and live state
argocd app diff <app-name>

# Sync an application (dry-run first)
argocd app sync <app-name> --dry-run
argocd app sync <app-name>

# View application history
argocd app history <app-name>

# Rollback to previous revision
argocd app rollback <app-name> <revision>
```

### Troubleshooting

```bash
# Check ArgoCD server health
argocd admin dashboard --port 8080 &
# Or check directly
kubectl get pods -n argocd -o wide

# View sync errors
argocd app get <app-name> --output json | jq '.status.conditions'

# List recently synced apps (last 2 hours)
argocd app list --output json | jq '[.[] | select(.status.operationState.finishedAt > "'$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ)'")] | .[] | {name: .metadata.name, sync: .status.sync.status, phase: .status.operationState.phase}'
```

### ArgoCD URLs

```
<ArgoCD URL>/applications/<app-name>                    # App details
<ArgoCD URL>/applications/<app-name>?resource=          # Resource tree
```

---

## GitHub Actions

### Workflow Management (via `gh` CLI)

```bash
# List recent workflow runs
gh run list --limit 20

# View a specific run
gh run view <run-id>

# View failed job logs
gh run view <run-id> --log-failed

# Re-run a failed workflow
gh run rerun <run-id>

# List workflow files
gh workflow list

# Trigger a workflow manually
gh workflow run <workflow-name> --ref <branch>
```

### Debugging Failed Pipelines

```bash
# Get the failure reason
gh run view <run-id> --json conclusion,jobs --jq '.jobs[] | select(.conclusion=="failure") | {name: .name, steps: [.steps[] | select(.conclusion=="failure") | {name: .name, conclusion: .conclusion}]}'

# Download logs
gh run download <run-id> --dir ./logs

# Check workflow file for syntax issues
gh workflow view <workflow-name>
```

### PR & Deployment Workflows

```bash
# Create a PR
gh pr create --title "..." --body "..." --base main --head feature-branch

# Check PR CI status
gh pr checks <pr-number>

# List deployments
gh api repos/{owner}/{repo}/deployments --jq '.[] | {env: .environment, sha: .sha[:8], created: .created_at}'
```

---

## General Pipeline Best Practices

- **Always check CI status** before investigating — `gh pr checks` or ArgoCD sync status
- **Read failure logs bottom-up** — the root cause is usually at the first error
- **Check for flaky tests** — if the same test fails intermittently, note it
- **Retry before debugging** — transient network issues cause ~30% of CI failures
- **Check recent changes** — `git log --oneline -20` to see what was recently merged
