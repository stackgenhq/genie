package pm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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
		endpoint = cfg.BaseURL
	}
	return &linearService{
		endpoint: endpoint,
		token:    cfg.APIToken,
		client:   &http.Client{Timeout: 15 * time.Second},
	}, nil
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

func (l *linearService) do(ctx context.Context, gql gqlRequest) (*gqlResponse, error) {
	data, _ := json.Marshal(gql)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pm/linear: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", l.token)

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/linear: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pm/linear: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("pm/linear: parse response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("pm/linear: graphql error: %s", gqlResp.Errors[0].Message)
	}
	return &gqlResp, nil
}

// ── Service implementation ──────────────────────────────────────────────

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
