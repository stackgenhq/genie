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
	"time"

	"github.com/stackgenhq/genie/pkg/httputil"
)

const asanaBaseURL = "https://app.asana.com/api/1.0"

// asanaService implements Service via the Asana REST API v1.
type asanaService struct {
	baseURL string
	token   string
	client  *http.Client
}

// newAsana creates an Asana-backed Service.
func newAsana(cfg Config) (*asanaService, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("pm/asana: api_token is required")
	}
	base := asanaBaseURL
	if cfg.BaseURL != "" {
		base = cfg.BaseURL
	}
	return &asanaService{
		baseURL: base,
		token:   cfg.APIToken,
		client:  httputil.GetClient(),
	}, nil
}

// ── Asana REST payloads ─────────────────────────────────────────────────

func (a *asanaService) Supported() []string {
	return []string{opGetIssue, opListIssues, opCreateIssue, opAssignIssue}
}

// Validate performs a lightweight health check against the Asana REST API
// by calling GET /users/me to verify the token and base URL.
func (a *asanaService) Validate(ctx context.Context) error {
	url := fmt.Sprintf("%s/users/me", a.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("pm/asana: build validate request: %w", err)
	}
	a.setAuth(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("pm/asana: validate request failed: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pm/asana: validate failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

type asanaTaskResponse struct {
	Data struct {
		GID      string `json:"gid"`
		Name     string `json:"name"`
		Notes    string `json:"notes"`
		Assignee *struct {
			Name string `json:"name"`
		} `json:"assignee"`
		AssigneeStatus string `json:"assignee_status"`
	} `json:"data"`
}

type asanaCreateResponse struct {
	Data struct {
		GID  string `json:"gid"`
		Name string `json:"name"`
	} `json:"data"`
}

type asanaErrorResponse struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ── Service implementation ──────────────────────────────────────────────

func (a *asanaService) ListIssues(ctx context.Context, filter IssueFilter) ([]*Issue, error) {
	// Asana uses /user_task_lists/me/tasks for the current user's tasks.
	// completed_since=now returns only incomplete tasks.
	url := fmt.Sprintf("%s/tasks?opt_fields=gid,name,notes,assignee,assignee_status,completed&assignee=me&workspace=me&completed_since=now&limit=50", a.baseURL)
	if filter.Status == "closed" {
		// For closed tasks, look back 30 days.
		since := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
		url = fmt.Sprintf("%s/tasks?opt_fields=gid,name,notes,assignee,assignee_status,completed&assignee=me&workspace=me&completed_since=%s&completed=true&limit=50", a.baseURL, since)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("pm/asana: build request: %w", err)
	}
	a.setAuth(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/asana: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError("list tasks", resp.StatusCode, body)
	}

	var result struct {
		Data []struct {
			GID      string `json:"gid"`
			Name     string `json:"name"`
			Notes    string `json:"notes"`
			Assignee *struct {
				Name string `json:"name"`
			} `json:"assignee"`
			AssigneeStatus string `json:"assignee_status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("pm/asana: parse response: %w", err)
	}

	issues := make([]*Issue, 0, len(result.Data))
	for _, d := range result.Data {
		issue := &Issue{
			ID:          d.GID,
			Title:       d.Name,
			Description: d.Notes,
			Status:      d.AssigneeStatus,
		}
		if d.Assignee != nil {
			issue.Assignee = d.Assignee.Name
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func (a *asanaService) GetIssue(ctx context.Context, id string) (*Issue, error) {
	url := fmt.Sprintf("%s/tasks/%s", a.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("pm/asana: build request: %w", err)
	}
	a.setAuth(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/asana: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError("GET task", resp.StatusCode, body)
	}

	var ar asanaTaskResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("pm/asana: parse response: %w", err)
	}

	issue := &Issue{
		ID:          ar.Data.GID,
		Title:       ar.Data.Name,
		Description: ar.Data.Notes,
		Status:      ar.Data.AssigneeStatus,
	}
	if ar.Data.Assignee != nil {
		issue.Assignee = ar.Data.Assignee.Name
	}
	return issue, nil
}

func (a *asanaService) CreateIssue(ctx context.Context, input IssueInput) (*Issue, error) {
	payload := map[string]any{
		"data": map[string]any{
			"name":     input.Title,
			"notes":    input.Description,
			"projects": []string{input.Project},
		},
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/tasks", a.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pm/asana: build request: %w", err)
	}
	a.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pm/asana: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, a.parseError("create task", resp.StatusCode, body)
	}

	var cr asanaCreateResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("pm/asana: parse response: %w", err)
	}

	return &Issue{
		ID:    cr.Data.GID,
		Title: cr.Data.Name,
	}, nil
}

func (a *asanaService) AssignIssue(ctx context.Context, id string, assignee string) error {
	payload := map[string]any{
		"data": map[string]any{
			"assignee": assignee,
		},
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/tasks/%s", a.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("pm/asana: build request: %w", err)
	}
	a.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("pm/asana: request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return a.parseError("assign task", resp.StatusCode, body)
	}
	return nil
}

func (a *asanaService) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
}

func (a *asanaService) parseError(op string, status int, body []byte) error {
	var ae asanaErrorResponse
	if err := json.Unmarshal(body, &ae); err == nil && len(ae.Errors) > 0 {
		return fmt.Errorf("pm/asana: %s: HTTP %d: %s", op, status, ae.Errors[0].Message)
	}
	return fmt.Errorf("pm/asana: %s: HTTP %d: %s", op, status, string(body))
}

func (a *asanaService) UpdateIssue(_ context.Context, _ string, _ IssueUpdate) (*Issue, error) {
	return nil, fmt.Errorf("pm/asana: UpdateIssue not implemented")
}
func (a *asanaService) AddComment(_ context.Context, _ string, _ string) (*Comment, error) {
	return nil, fmt.Errorf("pm/asana: AddComment not implemented")
}
func (a *asanaService) ListComments(_ context.Context, _ string) ([]*Comment, error) {
	return nil, fmt.Errorf("pm/asana: ListComments not implemented")
}
func (a *asanaService) SearchIssues(_ context.Context, _ string) ([]*Issue, error) {
	return nil, fmt.Errorf("pm/asana: SearchIssues not implemented")
}
func (a *asanaService) ListTeams(_ context.Context) ([]*Team, error) {
	return nil, fmt.Errorf("pm/asana: ListTeams not implemented")
}
func (a *asanaService) ListLabels(_ context.Context, _ string) ([]*Label, error) {
	return nil, fmt.Errorf("pm/asana: ListLabels not implemented")
}
func (a *asanaService) AddLabel(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("pm/asana: AddLabel not implemented")
}
func (a *asanaService) ListUsers(_ context.Context) ([]*User, error) {
	return nil, fmt.Errorf("pm/asana: ListUsers not implemented")
}
