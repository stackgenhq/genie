package atlassian

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	confluence "github.com/ctreminiom/go-atlassian/v2/confluence"
	jira "github.com/ctreminiom/go-atlassian/v2/jira/v3"
	"github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

// atlassianWrapper implements the Service interface using the
// ctreminiom/go-atlassian SDK (v2). This replaces the previous direct HTTP
// implementation, gaining typed request/response structs, ADF helpers, and
// automatic pagination.
type atlassianWrapper struct {
	jiraClient       *jira.Client
	confluenceClient *confluence.Client
}

// newWrapper constructs Jira and Confluence clients from the given Config.
func newWrapper(cfg Config) (*atlassianWrapper, error) {
	jiraClient, err := jira.New(nil, cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to create Jira client: %w", err)
	}
	jiraClient.Auth.SetBasicAuth(cfg.Email, cfg.Token)

	confluenceClient, err := confluence.New(nil, cfg.BaseURL+"/wiki")
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to create Confluence client: %w", err)
	}
	confluenceClient.Auth.SetBasicAuth(cfg.Email, cfg.Token)

	return &atlassianWrapper{
		jiraClient:       jiraClient,
		confluenceClient: confluenceClient,
	}, nil
}

// ── Jira Operations ─────────────────────────────────────────────────────

func (w *atlassianWrapper) JiraSearchIssues(ctx context.Context, jql string, maxResults int) ([]IssueSummary, error) {
	result, _, err := w.jiraClient.Issue.Search.Post(ctx, jql, nil, []string{"summary", "status", "issuetype"}, 0, maxResults, "") //nolint:staticcheck // TODO: migrate to SearchJQL after May 2025 deprecation
	if err != nil {
		return nil, fmt.Errorf("atlassian: jira search failed: %w", err)
	}

	summaries := make([]IssueSummary, 0, len(result.Issues))
	for _, issue := range result.Issues {
		summaries = append(summaries, IssueSummary{
			Key:     issue.Key,
			Summary: issue.Fields.Summary,
			Status:  issue.Fields.Status.Name,
			Type:    issue.Fields.IssueType.Name,
		})
	}
	return summaries, nil
}

func (w *atlassianWrapper) JiraGetIssue(ctx context.Context, issueKey string) (*IssueDetail, error) {
	issue, _, err := w.jiraClient.Issue.Get(ctx, issueKey, nil, []string{"transitions"})
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to get issue %s: %w", issueKey, err)
	}

	descStr := ""
	if issue.Fields.Description != nil {
		descBytes, _ := json.Marshal(issue.Fields.Description)
		descStr = string(descBytes)
	}

	assignee := ""
	if issue.Fields.Assignee != nil {
		assignee = issue.Fields.Assignee.DisplayName
	}
	reporter := ""
	if issue.Fields.Reporter != nil {
		reporter = issue.Fields.Reporter.DisplayName
	}
	priority := ""
	if issue.Fields.Priority != nil {
		priority = issue.Fields.Priority.Name
	}

	created := ""
	if issue.Fields.Created != nil {
		created = time.Time(*issue.Fields.Created).Format(time.RFC3339)
	}
	updated := ""
	if issue.Fields.Updated != nil {
		updated = time.Time(*issue.Fields.Updated).Format(time.RFC3339)
	}

	return &IssueDetail{
		Key:         issue.Key,
		Summary:     issue.Fields.Summary,
		Description: descStr,
		Status:      issue.Fields.Status.Name,
		Type:        issue.Fields.IssueType.Name,
		Priority:    priority,
		Assignee:    assignee,
		Reporter:    reporter,
		Labels:      issue.Fields.Labels,
		Created:     created,
		Updated:     updated,
	}, nil
}

func (w *atlassianWrapper) JiraCreateIssue(ctx context.Context, input CreateIssueInput) (*IssueSummary, error) {
	payload := &models.IssueScheme{
		Fields: &models.IssueFieldsScheme{
			Summary:   input.Summary,
			Project:   &models.ProjectScheme{Key: input.ProjectKey},
			IssueType: &models.IssueTypeScheme{Name: input.IssueType},
		},
	}

	if input.Description != "" {
		payload.Fields.Description = &models.CommentNodeScheme{
			Version: 1,
			Type:    "doc",
			Content: []*models.CommentNodeScheme{
				{
					Type: "paragraph",
					Content: []*models.CommentNodeScheme{
						{Type: "text", Text: input.Description},
					},
				},
			},
		}
	}

	created, _, err := w.jiraClient.Issue.Create(ctx, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to create issue: %w", err)
	}

	return &IssueSummary{
		Key:     created.Key,
		Summary: input.Summary,
		Type:    input.IssueType,
	}, nil
}

func (w *atlassianWrapper) JiraUpdateIssue(ctx context.Context, issueKey string, input UpdateIssueInput) error {
	payload := &models.IssueScheme{
		Fields: &models.IssueFieldsScheme{},
	}

	if input.Summary != "" {
		payload.Fields.Summary = input.Summary
	}
	if input.Description != "" {
		payload.Fields.Description = &models.CommentNodeScheme{
			Version: 1,
			Type:    "doc",
			Content: []*models.CommentNodeScheme{
				{
					Type: "paragraph",
					Content: []*models.CommentNodeScheme{
						{Type: "text", Text: input.Description},
					},
				},
			},
		}
	}

	_, err := w.jiraClient.Issue.Update(ctx, issueKey, false, payload, nil, nil)
	if err != nil {
		return fmt.Errorf("atlassian: failed to update issue %s: %w", issueKey, err)
	}
	return nil
}

func (w *atlassianWrapper) JiraAddComment(ctx context.Context, issueKey string, body string) error {
	commentPayload := &models.CommentPayloadScheme{
		Body: &models.CommentNodeScheme{
			Version: 1,
			Type:    "doc",
			Content: []*models.CommentNodeScheme{
				{
					Type: "paragraph",
					Content: []*models.CommentNodeScheme{
						{Type: "text", Text: body},
					},
				},
			},
		},
	}

	_, _, err := w.jiraClient.Issue.Comment.Add(ctx, issueKey, commentPayload, nil)
	if err != nil {
		return fmt.Errorf("atlassian: failed to add comment to %s: %w", issueKey, err)
	}
	return nil
}

func (w *atlassianWrapper) JiraListTransitions(ctx context.Context, issueKey string) ([]Transition, error) {
	result, _, err := w.jiraClient.Issue.Transitions(ctx, issueKey)
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to list transitions for %s: %w", issueKey, err)
	}

	transitions := make([]Transition, 0, len(result.Transitions))
	for _, t := range result.Transitions {
		transitions = append(transitions, Transition{ID: t.ID, Name: t.Name})
	}
	return transitions, nil
}

func (w *atlassianWrapper) JiraTransitionIssue(ctx context.Context, issueKey string, transitionID string) error {
	_, err := w.jiraClient.Issue.Move(ctx, issueKey, transitionID, nil)
	if err != nil {
		return fmt.Errorf("atlassian: failed to transition issue %s: %w", issueKey, err)
	}
	return nil
}

// ── Confluence Operations ───────────────────────────────────────────────

func (w *atlassianWrapper) ConfluenceSearch(ctx context.Context, cql string, maxResults int) ([]PageSummary, error) {
	result, _, err := w.confluenceClient.Content.Search(ctx, cql, "", []string{"space"}, "", maxResults)
	if err != nil {
		return nil, fmt.Errorf("atlassian: confluence search failed: %w", err)
	}

	pages := make([]PageSummary, 0, len(result.Results))
	for _, content := range result.Results {
		space := ""
		if content.Space != nil {
			space = content.Space.Key
		}
		pages = append(pages, PageSummary{
			ID:    content.ID,
			Title: content.Title,
			Space: space,
		})
	}
	return pages, nil
}

func (w *atlassianWrapper) ConfluenceGetPage(ctx context.Context, pageID string) (*PageDetail, error) {
	content, _, err := w.confluenceClient.Content.Get(ctx, pageID, []string{"body.storage", "version", "space"}, 0)
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to get page %s: %w", pageID, err)
	}

	body := ""
	if content.Body != nil && content.Body.Storage != nil {
		body = content.Body.Storage.Value
	}
	space := ""
	if content.Space != nil {
		space = content.Space.Key
	}
	version := 0
	if content.Version != nil {
		version = content.Version.Number
	}

	return &PageDetail{
		ID:      content.ID,
		Title:   content.Title,
		Space:   space,
		Body:    body,
		Version: version,
	}, nil
}

func (w *atlassianWrapper) ConfluenceCreatePage(ctx context.Context, input CreatePageInput) (*PageSummary, error) {
	payload := &models.ContentScheme{
		Type:  "page",
		Title: input.Title,
		Space: &models.SpaceScheme{Key: input.SpaceKey},
		Body: &models.BodyScheme{
			Storage: &models.BodyNodeScheme{
				Value:          input.Body,
				Representation: "storage",
			},
		},
	}

	if input.ParentID != "" {
		payload.Ancestors = []*models.ContentScheme{{ID: input.ParentID}}
	}

	created, _, err := w.confluenceClient.Content.Create(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("atlassian: failed to create page: %w", err)
	}

	return &PageSummary{
		ID:    created.ID,
		Title: created.Title,
		Space: input.SpaceKey,
	}, nil
}

func (w *atlassianWrapper) ConfluenceUpdatePage(ctx context.Context, pageID string, input UpdatePageInput) error {
	payload := &models.ContentScheme{
		Type:  "page",
		Title: input.Title,
		Version: &models.ContentVersionScheme{
			Number: input.Version + 1,
		},
		Body: &models.BodyScheme{
			Storage: &models.BodyNodeScheme{
				Value:          input.Body,
				Representation: "storage",
			},
		},
	}

	_, _, err := w.confluenceClient.Content.Update(ctx, pageID, payload)
	if err != nil {
		return fmt.Errorf("atlassian: failed to update page %s: %w", pageID, err)
	}
	return nil
}

func (w *atlassianWrapper) Validate(ctx context.Context) error {
	_, _, err := w.jiraClient.MySelf.Details(ctx, nil)
	if err != nil {
		return fmt.Errorf("atlassian: validate failed: %w", err)
	}
	return nil
}
