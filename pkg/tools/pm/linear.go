package pm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/appcd-dev/go-lib/httputil"
	"github.com/stackgenhq/genie/pkg/retrier"
)

const linearEndpoint = "https://api.linear.app/graphql"

// linearService implements Service via the Linear GraphQL API.
type linearService struct {
	endpoint string // defaults to linearEndpoint, overridable for testing
	token    string
	client   *http.Client
}

// newLinear creates a Linear-backed Service.
func newLinear(cfg Config) (*linearService, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("pm/linear: api_token is required")
	}
	endpoint := linearEndpoint
	if cfg.BaseURL != "" {
		// Auto-correct common misconfiguration: the Linear website URL
		// (https://linear.app/) is not the API endpoint.
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "https://linear.app" || base == "http://linear.app" {
			endpoint = linearEndpoint
		} else {
			endpoint = cfg.BaseURL
		}
	}
	return &linearService{
		endpoint: endpoint,
		token:    cfg.APIToken,
		client:   httputil.GetClient(),
	}, nil
}

func (l *linearService) Supported() []string {
	return []string{
		opGetIssue, opListIssues, opCreateIssue, opAssignIssue,
		opUpdateIssue, opAddComment, opListComments, opSearchIssues,
		opListTeams, opListLabels, opAddLabel, opListUsers,
	}
}

// Validate performs a lightweight health check against the Linear API
// by sending a `{ viewer { id } }` query. It surfaces token or endpoint
// problems early rather than waiting for the first real tool call.
func (l *linearService) Validate(ctx context.Context) error {
	_, err := l.do(ctx, gqlRequest{
		Query: `{ viewer { id } }`,
	})
	return err
}

// ── GraphQL helpers ─────────────────────────────────────────────────────

type gqlRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// isRetryable returns true for transient HTTP status codes.
func isRetryable(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusInternalServerError ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func (l *linearService) do(ctx context.Context, gql gqlRequest) (*gqlResponse, error) {
	data, err := json.Marshal(gql)
	if err != nil {
		return nil, fmt.Errorf("pm/linear: marshal request: %w", err)
	}

	var gqlResp *gqlResponse
	err = retrier.Retry(ctx, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(data))
		if reqErr != nil {
			return fmt.Errorf("pm/linear: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", l.token)

		resp, doErr := l.client.Do(req)
		if doErr != nil {
			return fmt.Errorf("pm/linear: request failed: %w", doErr)
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if isRetryable(resp.StatusCode) {
			return fmt.Errorf("pm/linear: HTTP %d: %s", resp.StatusCode, string(body))
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("pm/linear: HTTP %d: %s", resp.StatusCode, string(body))
		}

		var parsed gqlResponse
		if unmarshalErr := json.Unmarshal(body, &parsed); unmarshalErr != nil {
			return fmt.Errorf("pm/linear: parse response: %w", unmarshalErr)
		}
		if len(parsed.Errors) > 0 {
			return fmt.Errorf("pm/linear: graphql error: %s", parsed.Errors[0].Message)
		}
		gqlResp = &parsed
		return nil
	}, retrier.WithAttempts(3), retrier.WithBackoffDuration(500*time.Millisecond))

	if err != nil {
		return nil, err
	}
	return gqlResp, nil
}

// ── Service implementation ──────────────────────────────────────────────

func (l *linearService) ListIssues(ctx context.Context, filter IssueFilter) ([]*Issue, error) {
	// Default to open issues (unstarted + started).
	stateFilter := `["unstarted","started"]`
	if filter.Status == "closed" {
		stateFilter = `["completed","canceled"]`
	}

	gql := gqlRequest{
		Query: fmt.Sprintf(`{
			issues(
				first: 50
				filter: { state: { type: { in: %s } } }
			) {
				nodes {
					identifier
					title
					description
					state { name }
					assignee { name }
				}
			}
		}`, stateFilter),
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		Issues struct {
			Nodes []struct {
				Identifier  string `json:"identifier"`
				Title       string `json:"title"`
				Description string `json:"description"`
				State       struct {
					Name string `json:"name"`
				} `json:"state"`
				Assignee *struct {
					Name string `json:"name"`
				} `json:"assignee"`
			} `json:"nodes"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse issues: %w", err)
	}

	issues := make([]*Issue, 0, len(result.Issues.Nodes))
	for _, n := range result.Issues.Nodes {
		issue := &Issue{
			ID:          n.Identifier,
			Title:       n.Title,
			Description: n.Description,
			Status:      n.State.Name,
		}
		if n.Assignee != nil {
			issue.Assignee = n.Assignee.Name
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func (l *linearService) GetIssue(ctx context.Context, id string) (*Issue, error) {
	gql := gqlRequest{
		Query: `query($id: String!) {
			issue(id: $id) {
				id
				title
				description
				state { name }
				assignee { name }
			}
		}`,
		Variables: map[string]string{"id": id},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		Issue struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			State       struct {
				Name string `json:"name"`
			} `json:"state"`
			Assignee *struct {
				Name string `json:"name"`
			} `json:"assignee"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse issue: %w", err)
	}

	issue := &Issue{
		ID:          result.Issue.ID,
		Title:       result.Issue.Title,
		Description: result.Issue.Description,
		Status:      result.Issue.State.Name,
	}
	if result.Issue.Assignee != nil {
		issue.Assignee = result.Issue.Assignee.Name
	}
	return issue, nil
}

func (l *linearService) CreateIssue(ctx context.Context, input IssueInput) (*Issue, error) {
	gql := gqlRequest{
		Query: `mutation($title: String!, $description: String, $teamId: String!) {
			issueCreate(input: { title: $title, description: $description, teamId: $teamId }) {
				success
				issue { id title }
			}
		}`,
		Variables: map[string]string{
			"title":       input.Title,
			"description": input.Description,
			"teamId":      input.Project,
		},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		IssueCreate struct {
			Success bool `json:"success"`
			Issue   struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse create response: %w", err)
	}
	if !result.IssueCreate.Success {
		return nil, fmt.Errorf("pm/linear: issue creation failed")
	}

	return &Issue{
		ID:    result.IssueCreate.Issue.ID,
		Title: result.IssueCreate.Issue.Title,
	}, nil
}

func (l *linearService) AssignIssue(ctx context.Context, id string, assignee string) error {
	gql := gqlRequest{
		Query: `mutation($id: String!, $assigneeId: String!) {
			issueUpdate(id: $id, input: { assigneeId: $assigneeId }) {
				success
			}
		}`,
		Variables: map[string]string{
			"id":         id,
			"assigneeId": assignee,
		},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return err
	}

	var result struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return fmt.Errorf("pm/linear: parse assign response: %w", err)
	}
	if !result.IssueUpdate.Success {
		return fmt.Errorf("pm/linear: assign failed")
	}
	return nil
}

func (l *linearService) UpdateIssue(ctx context.Context, id string, update IssueUpdate) (*Issue, error) {
	// Build the input fields dynamically.
	input := make(map[string]any)
	if update.Title != nil {
		input["title"] = *update.Title
	}
	if update.Description != nil {
		input["description"] = *update.Description
	}
	if update.Status != nil {
		// Linear uses stateId, not state name. Look up the workflow state first.
		stateID, err := l.resolveStateID(ctx, *update.Status)
		if err != nil {
			return nil, err
		}
		input["stateId"] = stateID
	}

	gql := gqlRequest{
		Query: `mutation($id: String!, $input: IssueUpdateInput!) {
			issueUpdate(id: $id, input: $input) {
				success
				issue { identifier title description state { name } assignee { name } }
			}
		}`,
		Variables: map[string]any{"id": id, "input": input},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				Identifier  string `json:"identifier"`
				Title       string `json:"title"`
				Description string `json:"description"`
				State       struct {
					Name string `json:"name"`
				} `json:"state"`
				Assignee *struct {
					Name string `json:"name"`
				} `json:"assignee"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse update response: %w", err)
	}
	if !result.IssueUpdate.Success {
		return nil, fmt.Errorf("pm/linear: update issue failed")
	}

	issue := &Issue{
		ID:          result.IssueUpdate.Issue.Identifier,
		Title:       result.IssueUpdate.Issue.Title,
		Description: result.IssueUpdate.Issue.Description,
		Status:      result.IssueUpdate.Issue.State.Name,
	}
	if result.IssueUpdate.Issue.Assignee != nil {
		issue.Assignee = result.IssueUpdate.Issue.Assignee.Name
	}
	return issue, nil
}

// resolveStateID finds a workflow state ID by name (case-insensitive prefix match).
func (l *linearService) resolveStateID(ctx context.Context, stateName string) (string, error) {
	gql := gqlRequest{
		Query: `{ workflowStates(first: 100) { nodes { id name } } }`,
	}
	resp, err := l.do(ctx, gql)
	if err != nil {
		return "", err
	}
	var result struct {
		WorkflowStates struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("pm/linear: parse states: %w", err)
	}
	lower := strings.ToLower(stateName)
	for _, s := range result.WorkflowStates.Nodes {
		if strings.ToLower(s.Name) == lower {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("pm/linear: workflow state %q not found", stateName)
}

func (l *linearService) AddComment(ctx context.Context, issueID string, body string) (*Comment, error) {
	gql := gqlRequest{
		Query: `mutation($issueId: String!, $body: String!) {
			commentCreate(input: { issueId: $issueId, body: $body }) {
				success
				comment { id body user { name } }
			}
		}`,
		Variables: map[string]string{"issueId": issueID, "body": body},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		CommentCreate struct {
			Success bool `json:"success"`
			Comment struct {
				ID   string `json:"id"`
				Body string `json:"body"`
				User *struct {
					Name string `json:"name"`
				} `json:"user"`
			} `json:"comment"`
		} `json:"commentCreate"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse comment response: %w", err)
	}
	if !result.CommentCreate.Success {
		return nil, fmt.Errorf("pm/linear: create comment failed")
	}

	c := &Comment{
		ID:   result.CommentCreate.Comment.ID,
		Body: result.CommentCreate.Comment.Body,
	}
	if result.CommentCreate.Comment.User != nil {
		c.Author = result.CommentCreate.Comment.User.Name
	}
	return c, nil
}

func (l *linearService) ListComments(ctx context.Context, issueID string) ([]*Comment, error) {
	gql := gqlRequest{
		Query: `query($id: String!) {
			issue(id: $id) {
				comments(first: 50) {
					nodes { id body user { name } }
				}
			}
		}`,
		Variables: map[string]string{"id": issueID},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		Issue struct {
			Comments struct {
				Nodes []struct {
					ID   string `json:"id"`
					Body string `json:"body"`
					User *struct {
						Name string `json:"name"`
					} `json:"user"`
				} `json:"nodes"`
			} `json:"comments"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse comments: %w", err)
	}

	comments := make([]*Comment, 0, len(result.Issue.Comments.Nodes))
	for _, n := range result.Issue.Comments.Nodes {
		c := &Comment{ID: n.ID, Body: n.Body}
		if n.User != nil {
			c.Author = n.User.Name
		}
		comments = append(comments, c)
	}
	return comments, nil
}

func (l *linearService) SearchIssues(ctx context.Context, query string) ([]*Issue, error) {
	gql := gqlRequest{
		Query: `query($term: String!) {
			searchIssues(term: $term, first: 50) {
				nodes {
					identifier title description
					state { name }
					assignee { name }
				}
			}
		}`,
		Variables: map[string]string{"term": query},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		SearchIssues struct {
			Nodes []struct {
				Identifier  string `json:"identifier"`
				Title       string `json:"title"`
				Description string `json:"description"`
				State       struct {
					Name string `json:"name"`
				} `json:"state"`
				Assignee *struct {
					Name string `json:"name"`
				} `json:"assignee"`
			} `json:"nodes"`
		} `json:"searchIssues"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse search: %w", err)
	}

	issues := make([]*Issue, 0, len(result.SearchIssues.Nodes))
	for _, n := range result.SearchIssues.Nodes {
		issue := &Issue{
			ID: n.Identifier, Title: n.Title, Description: n.Description, Status: n.State.Name,
		}
		if n.Assignee != nil {
			issue.Assignee = n.Assignee.Name
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func (l *linearService) ListTeams(ctx context.Context) ([]*Team, error) {
	gql := gqlRequest{
		Query: `{ teams(first: 100) { nodes { id name key } } }`,
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		Teams struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Key  string `json:"key"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse teams: %w", err)
	}

	teams := make([]*Team, 0, len(result.Teams.Nodes))
	for _, n := range result.Teams.Nodes {
		teams = append(teams, &Team{ID: n.ID, Name: n.Name, Key: n.Key})
	}
	return teams, nil
}

func (l *linearService) ListLabels(ctx context.Context, teamID string) ([]*Label, error) {
	gql := gqlRequest{
		Query: `query($teamId: String!) {
			team(id: $teamId) {
				labels(first: 100) {
					nodes { id name color }
				}
			}
		}`,
		Variables: map[string]string{"teamId": teamID},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		Team struct {
			Labels struct {
				Nodes []struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Color string `json:"color"`
				} `json:"nodes"`
			} `json:"labels"`
		} `json:"team"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse labels: %w", err)
	}

	labels := make([]*Label, 0, len(result.Team.Labels.Nodes))
	for _, n := range result.Team.Labels.Nodes {
		labels = append(labels, &Label{ID: n.ID, Name: n.Name, Color: n.Color})
	}
	return labels, nil
}

func (l *linearService) AddLabel(ctx context.Context, issueID string, labelID string) error {
	// Linear's issueAddLabel mutation adds a label to an issue.
	gql := gqlRequest{
		Query: `mutation($issueId: String!, $labelId: String!) {
			issueAddLabel(id: $issueId, labelId: $labelId) { success }
		}`,
		Variables: map[string]string{"issueId": issueID, "labelId": labelID},
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return err
	}

	var result struct {
		IssueAddLabel struct {
			Success bool `json:"success"`
		} `json:"issueAddLabel"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return fmt.Errorf("pm/linear: parse add label: %w", err)
	}
	if !result.IssueAddLabel.Success {
		return fmt.Errorf("pm/linear: add label failed")
	}
	return nil
}

func (l *linearService) ListUsers(ctx context.Context) ([]*User, error) {
	gql := gqlRequest{
		Query: `{ users(first: 100) { nodes { id name email } } }`,
	}

	resp, err := l.do(ctx, gql)
	if err != nil {
		return nil, err
	}

	var result struct {
		Users struct {
			Nodes []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"nodes"`
		} `json:"users"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("pm/linear: parse users: %w", err)
	}

	users := make([]*User, 0, len(result.Users.Nodes))
	for _, n := range result.Users.Nodes {
		users = append(users, &User{ID: n.ID, Name: n.Name, Email: n.Email})
	}
	return users, nil
}
