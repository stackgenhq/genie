// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/stackgenhq/genie/pkg/httputil"
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
		client:  httputil.GetClient(),
	}, nil
}

// ── Jira REST payloads ──────────────────────────────────────────────────

func (j *jiraService) Supported() []string {
	return []string{opGetIssue, opListIssues, opCreateIssue, opAssignIssue}
}

// Validate performs a lightweight health check against the Jira REST API
// by calling GET /rest/api/3/myself to verify the token and base URL.
func (j *jiraService) Validate(ctx context.Context) error {
	url := fmt.Sprintf("%s/rest/api/3/myself", j.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("pm/jira: build validate request: %w", err)
	}
	j.setAuth(req)

	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("pm/jira: validate request failed: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pm/jira: validate failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

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

func (j *jiraService) ListIssues(ctx context.Context, filter IssueFilter) ([]*Issue, error) {
	jql := "statusCategory != Done ORDER BY updated DESC"
	if filter.Status == "closed" {
		jql = "statusCategory = Done ORDER BY updated DESC"
	}

	url := fmt.Sprintf("%s/rest/api/3/search?jql=%s&maxResults=50&fields=summary,status,assignee,description",
		j.baseURL, strings.ReplaceAll(jql, " ", "+"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("pm/jira: build request: %w", err)
	}
	j.setAuth(req)

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/jira: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pm/jira: search returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Issues []jiraIssueResponse `json:"issues"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("pm/jira: parse search response: %w", err)
	}

	issues := make([]*Issue, 0, len(result.Issues))
	for _, jr := range result.Issues {
		issue := &Issue{
			ID:     jr.Key,
			Title:  jr.Fields.Summary,
			Status: jr.Fields.Status.Name,
		}
		if jr.Fields.Assignee != nil {
			issue.Assignee = jr.Fields.Assignee.DisplayName
		}
		issue.Description = extractJiraDescription(jr.Fields.Description)
		issues = append(issues, issue)
	}
	return issues, nil
}

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
	defer func() { _ = resp.Body.Close() }()

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
	issue.Description = extractJiraDescription(jr.Fields.Description)
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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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

// extractJiraDescription converts a Jira v3 description field to a string.
// Jira returns ADF (Atlassian Document Format) objects for rich descriptions,
// not plain strings. This helper gracefully handles both cases.
func extractJiraDescription(desc any) string {
	if desc == nil {
		return ""
	}
	if s, ok := desc.(string); ok {
		return s
	}
	// ADF object — marshal to JSON as a best-effort representation.
	raw, err := json.Marshal(desc)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (j *jiraService) UpdateIssue(_ context.Context, _ string, _ IssueUpdate) (*Issue, error) {
	return nil, fmt.Errorf("pm/jira: UpdateIssue not implemented")
}
func (j *jiraService) AddComment(_ context.Context, _ string, _ string) (*Comment, error) {
	return nil, fmt.Errorf("pm/jira: AddComment not implemented")
}
func (j *jiraService) ListComments(_ context.Context, _ string) ([]*Comment, error) {
	return nil, fmt.Errorf("pm/jira: ListComments not implemented")
}
func (j *jiraService) SearchIssues(_ context.Context, _ string) ([]*Issue, error) {
	return nil, fmt.Errorf("pm/jira: SearchIssues not implemented")
}
func (j *jiraService) ListTeams(_ context.Context) ([]*Team, error) {
	return nil, fmt.Errorf("pm/jira: ListTeams not implemented")
}
func (j *jiraService) ListLabels(_ context.Context, _ string) ([]*Label, error) {
	return nil, fmt.Errorf("pm/jira: ListLabels not implemented")
}
func (j *jiraService) AddLabel(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("pm/jira: AddLabel not implemented")
}
func (j *jiraService) ListUsers(_ context.Context) ([]*User, error) {
	return nil, fmt.Errorf("pm/jira: ListUsers not implemented")
}
