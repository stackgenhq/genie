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
)

// jiraService implements Service via the Jira REST API v3.
type jiraService struct {
	baseURL string // e.g. https://mycompany.atlassian.net
	email   string
	token   string
	client  *http.Client
}

// newJira creates a Jira-backed Service.
func newJira(cfg Config) (*jiraService, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("pm/jira: base_url is required (e.g. https://mycompany.atlassian.net)")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("pm/jira: api_token is required")
	}
	if cfg.Email == "" {
		return nil, fmt.Errorf("pm/jira: email is required for Basic auth")
	}
	return &jiraService{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		email:   cfg.Email,
		token:   cfg.APIToken,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// ── Jira REST payloads ──────────────────────────────────────────────────

type jiraIssueResponse struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string `json:"summary"`
		Description any    `json:"description"` // ADF or nil
		Status      struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee *struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
	} `json:"fields"`
}

type jiraCreateRequest struct {
	Fields jiraCreateFields `json:"fields"`
}

type jiraCreateFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description,omitempty"`
	Project     struct {
		Key string `json:"key"`
	} `json:"project"`
	IssueType struct {
		Name string `json:"name"`
	} `json:"issuetype"`
}

type jiraCreateResponse struct {
	Key string `json:"key"`
}

// ── Service implementation ──────────────────────────────────────────────

func (j *jiraService) GetIssue(ctx context.Context, id string) (*Issue, error) {
	url := fmt.Sprintf("%s/rest/api/3/issue/%s", j.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("pm/jira: build request: %w", err)
	}
	j.setAuth(req)

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/jira: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pm/jira: GET issue %s returned HTTP %d: %s", id, resp.StatusCode, string(body))
	}

	var jr jiraIssueResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		return nil, fmt.Errorf("pm/jira: parse response: %w", err)
	}

	issue := &Issue{
		ID:     jr.Key,
		Title:  jr.Fields.Summary,
		Status: jr.Fields.Status.Name,
	}
	if jr.Fields.Assignee != nil {
		issue.Assignee = jr.Fields.Assignee.DisplayName
	}
	if jr.Fields.Description != nil {
		if desc, ok := jr.Fields.Description.(string); ok {
			issue.Description = desc
		}
	}
	return issue, nil
}

func (j *jiraService) CreateIssue(ctx context.Context, input IssueInput) (*Issue, error) {
	payload := jiraCreateRequest{}
	payload.Fields.Summary = input.Title
	payload.Fields.Project.Key = input.Project
	payload.Fields.IssueType.Name = input.Type

	if input.Description != "" {
		// Wrap description in Atlassian Document Format (ADF).
		adf := map[string]any{
			"type":    "doc",
			"version": 1,
			"content": []any{
				map[string]any{
					"type": "paragraph",
					"content": []any{
						map[string]any{
							"type": "text",
							"text": input.Description,
						},
					},
				},
			},
		}
		raw, _ := json.Marshal(adf)
		payload.Fields.Description = raw
	}

	data, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/rest/api/3/issue", j.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pm/jira: build request: %w", err)
	}
	j.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/jira: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("pm/jira: create issue returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var cr jiraCreateResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("pm/jira: parse response: %w", err)
	}

	return &Issue{
		ID:    cr.Key,
		Title: input.Title,
	}, nil
}

func (j *jiraService) AssignIssue(ctx context.Context, id string, assignee string) error {
	payload, _ := json.Marshal(map[string]string{"accountId": assignee})
	url := fmt.Sprintf("%s/rest/api/3/issue/%s/assignee", j.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("pm/jira: build request: %w", err)
	}
	j.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("pm/jira: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pm/jira: assign issue %s returned HTTP %d: %s", id, resp.StatusCode, string(body))
	}
	return nil
}

func (j *jiraService) setAuth(req *http.Request) {
	req.SetBasicAuth(j.email, j.token)
	req.Header.Set("Accept", "application/json")
}
