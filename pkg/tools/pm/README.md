# pkg/tools/pm — Project Management Tools

Unified interface for issue tracking across **Linear**, **Jira**, and **Asana**.

## Tools

| Tool Name | Description |
|---|---|
| `projectmanagement_get_issue` | Get details of a single issue by ID (e.g. `PROJ-123`) |
| `projectmanagement_list_issues` | List issues. Returns open issues by default; use `status=closed` for completed |
| `projectmanagement_create_issue` | Create a new issue with title, description, project, and type |
| `projectmanagement_assign_issue` | Assign an issue to a user |

## Configuration

In `.genie.toml`:

```toml
[project_management]
provider = "linear"          # linear, jira, or asana
api_token = "${PM_API_TOKEN}"
base_url = "https://linear.app/"  # Required for Jira; optional for Linear/Asana
# email = "user@example.com" # Jira only: required for Basic auth
```

## Service Interface

```go
type Service interface {
    GetIssue(ctx context.Context, id string) (*Issue, error)
    ListIssues(ctx context.Context, filter IssueFilter) ([]*Issue, error)
    CreateIssue(ctx context.Context, input IssueInput) (*Issue, error)
    AssignIssue(ctx context.Context, id string, assignee string) error
}
```

## Provider Details

- **Linear** — GraphQL API (`https://api.linear.app/graphql`). `ListIssues` filters by state type (`unstarted`/`started` for open, `completed`/`canceled` for closed).
- **Jira** — REST API v3. `ListIssues` uses JQL search (`statusCategory != Done`).
- **Asana** — REST API v1. `ListIssues` uses the tasks endpoint with `completed_since=now` for open tasks.
