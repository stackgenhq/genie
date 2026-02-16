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
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// ── Asana REST payloads ─────────────────────────────────────────────────

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
