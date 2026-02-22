package atlassian

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the Atlassian connector, covering
// both Jira and Confluence operations. Having a single interface for both
// products is intentional — go-atlassian provides a unified client, and
// keeping them together avoids duplicating auth/config wiring.
//
//counterfeiter:generate . Service
type Service interface {
	// Jira operations
	JiraSearchIssues(ctx context.Context, jql string, maxResults int) ([]IssueSummary, error)
	JiraGetIssue(ctx context.Context, issueKey string) (*IssueDetail, error)
	JiraCreateIssue(ctx context.Context, input CreateIssueInput) (*IssueSummary, error)
	JiraUpdateIssue(ctx context.Context, issueKey string, input UpdateIssueInput) error
	JiraAddComment(ctx context.Context, issueKey string, body string) error
	JiraListTransitions(ctx context.Context, issueKey string) ([]Transition, error)
	JiraTransitionIssue(ctx context.Context, issueKey string, transitionID string) error

	// Confluence operations
	ConfluenceSearch(ctx context.Context, cql string, maxResults int) ([]PageSummary, error)
	ConfluenceGetPage(ctx context.Context, pageID string) (*PageDetail, error)
	ConfluenceCreatePage(ctx context.Context, input CreatePageInput) (*PageSummary, error)
	ConfluenceUpdatePage(ctx context.Context, pageID string, input UpdatePageInput) error

	// Validate performs a lightweight health check to verify credentials.
	Validate(ctx context.Context) error
}

// Config holds configuration for Atlassian Cloud or Data Center.
type Config struct {
	BaseURL string `yaml:"base_url" toml:"base_url"` // e.g. https://yourco.atlassian.net
	Email   string `yaml:"email" toml:"email"`       // Atlassian account email
	Token   string `yaml:"token" toml:"token"`       // API token
}

// ── Domain Types ────────────────────────────────────────────────────────

// IssueSummary is a simplified Jira issue for listing.
type IssueSummary struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
	Type    string `json:"type"`
}

// IssueDetail is a detailed Jira issue response.
type IssueDetail struct {
	Key         string   `json:"key"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Type        string   `json:"type"`
	Priority    string   `json:"priority"`
	Assignee    string   `json:"assignee"`
	Reporter    string   `json:"reporter"`
	Labels      []string `json:"labels"`
	Created     string   `json:"created"`
	Updated     string   `json:"updated"`
}

// CreateIssueInput is the input for creating a Jira issue.
type CreateIssueInput struct {
	ProjectKey  string `json:"project_key" jsonschema:"description=Jira project key (e.g. PROJ),required"`
	Summary     string `json:"summary" jsonschema:"description=Issue summary/title,required"`
	Description string `json:"description" jsonschema:"description=Issue description"`
	IssueType   string `json:"issue_type" jsonschema:"description=Issue type (e.g. Bug/Story/Task),required"`
}

// UpdateIssueInput is the input for updating a Jira issue.
type UpdateIssueInput struct {
	Summary     string `json:"summary" jsonschema:"description=New summary (leave empty to keep current)"`
	Description string `json:"description" jsonschema:"description=New description (leave empty to keep current)"`
}

// Transition represents a Jira workflow transition.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PageSummary is a simplified Confluence page for listing.
type PageSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Space string `json:"space"`
}

// PageDetail is a detailed Confluence page response.
type PageDetail struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Space   string `json:"space"`
	Body    string `json:"body"`
	Version int    `json:"version"`
}

// CreatePageInput is the input for creating a Confluence page.
type CreatePageInput struct {
	SpaceKey string `json:"space_key" jsonschema:"description=Confluence space key,required"`
	Title    string `json:"title" jsonschema:"description=Page title,required"`
	Body     string `json:"body" jsonschema:"description=Page body in storage format (HTML),required"`
	ParentID string `json:"parent_id" jsonschema:"description=Parent page ID (optional)"`
}

// UpdatePageInput is the input for updating a Confluence page.
type UpdatePageInput struct {
	Title   string `json:"title" jsonschema:"description=New page title,required"`
	Body    string `json:"body" jsonschema:"description=New page body in storage format (HTML),required"`
	Version int    `json:"version" jsonschema:"description=Current page version number (for optimistic locking),required"`
}

// ── Factory ─────────────────────────────────────────────────────────────

// New creates a new Atlassian Service based on the configuration.
// It initialises both Jira and Confluence clients from a single set of
// credentials. Without this factory, callers would need to manage two
// separate client configurations.
func New(cfg Config) (Service, error) {
	log := logger.GetLogger(context.Background())
	log.Info("Initializing Atlassian service", "base_url", cfg.BaseURL, "has_token", cfg.Token != "")

	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("atlassian: base_url is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("atlassian: token is required")
	}
	if cfg.Email == "" {
		return nil, fmt.Errorf("atlassian: email is required")
	}

	return newWrapper(cfg)
}

// ── Request Types for Tools ─────────────────────────────────────────────

type jiraSearchRequest struct {
	JQL        string `json:"jql" jsonschema:"description=JQL query string (e.g. project=PROJ AND status=Open),required"`
	MaxResults int    `json:"max_results" jsonschema:"description=Maximum number of results (default 20)"`
}

type jiraGetIssueRequest struct {
	IssueKey string `json:"issue_key" jsonschema:"description=Jira issue key (e.g. PROJ-123),required"`
}

type jiraUpdateIssueRequest struct {
	IssueKey    string `json:"issue_key" jsonschema:"description=Jira issue key (e.g. PROJ-123),required"`
	Summary     string `json:"summary" jsonschema:"description=New summary (leave empty to keep current)"`
	Description string `json:"description" jsonschema:"description=New description (leave empty to keep current)"`
}

type jiraAddCommentRequest struct {
	IssueKey string `json:"issue_key" jsonschema:"description=Jira issue key (e.g. PROJ-123),required"`
	Body     string `json:"body" jsonschema:"description=Comment body text,required"`
}

type jiraTransitionRequest struct {
	IssueKey     string `json:"issue_key" jsonschema:"description=Jira issue key (e.g. PROJ-123),required"`
	TransitionID string `json:"transition_id" jsonschema:"description=Transition ID (use jira_list_transitions to discover),required"`
}

type confluenceSearchRequest struct {
	CQL        string `json:"cql" jsonschema:"description=CQL query string (e.g. space=DEV AND type=page AND text~search),required"`
	MaxResults int    `json:"max_results" jsonschema:"description=Maximum number of results (default 20)"`
}

type confluenceGetPageRequest struct {
	PageID string `json:"page_id" jsonschema:"description=Confluence page ID,required"`
}

type confluenceUpdatePageRequest struct {
	PageID  string `json:"page_id" jsonschema:"description=Confluence page ID,required"`
	Title   string `json:"title" jsonschema:"description=New page title,required"`
	Body    string `json:"body" jsonschema:"description=New page body in storage format (HTML),required"`
	Version int    `json:"version" jsonschema:"description=Current page version number (for optimistic locking),required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewJiraSearchTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraSearch,
		function.WithName("jira_search_issues"),
		function.WithDescription("Search Jira issues using JQL. Returns issue keys, summaries, statuses, and types."),
	)
}

func NewJiraGetIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraGetIssue,
		function.WithName("jira_get_issue"),
		function.WithDescription("Get detailed information about a specific Jira issue including description, assignee, and labels."),
	)
}

func NewJiraCreateIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraCreateIssue,
		function.WithName("jira_create_issue"),
		function.WithDescription("Create a new Jira issue in a project."),
	)
}

func NewJiraUpdateIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraUpdateIssue,
		function.WithName("jira_update_issue"),
		function.WithDescription("Update an existing Jira issue's summary or description."),
	)
}

func NewJiraAddCommentTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraAddComment,
		function.WithName("jira_add_comment"),
		function.WithDescription("Add a comment to a Jira issue."),
	)
}

func NewJiraListTransitionsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraListTransitions,
		function.WithName("jira_list_transitions"),
		function.WithDescription("List available workflow transitions for a Jira issue. Use this to discover valid transition IDs before transitioning."),
	)
}

func NewJiraTransitionIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.jiraTransitionIssue,
		function.WithName("jira_transition_issue"),
		function.WithDescription("Transition a Jira issue to a new status using a transition ID."),
	)
}

func NewConfluenceSearchTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.confluenceSearch,
		function.WithName("confluence_search"),
		function.WithDescription("Search Confluence pages using CQL. Returns page IDs, titles, and spaces."),
	)
}

func NewConfluenceGetPageTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.confluenceGetPage,
		function.WithName("confluence_get_page"),
		function.WithDescription("Get the full content of a Confluence page including its body."),
	)
}

func NewConfluenceCreatePageTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.confluenceCreatePage,
		function.WithName("confluence_create_page"),
		function.WithDescription("Create a new Confluence page in a space."),
	)
}

func NewConfluenceUpdatePageTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.confluenceUpdatePage,
		function.WithName("confluence_update_page"),
		function.WithDescription("Update an existing Confluence page's title and body."),
	)
}

// AllTools returns all Atlassian (Jira + Confluence) tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewJiraSearchTool(s),
		NewJiraGetIssueTool(s),
		NewJiraCreateIssueTool(s),
		NewJiraUpdateIssueTool(s),
		NewJiraAddCommentTool(s),
		NewJiraListTransitionsTool(s),
		NewJiraTransitionIssueTool(s),
		NewConfluenceSearchTool(s),
		NewConfluenceGetPageTool(s),
		NewConfluenceCreatePageTool(s),
		NewConfluenceUpdatePageTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) jiraSearch(ctx context.Context, req jiraSearchRequest) ([]IssueSummary, error) {
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}
	return ts.s.JiraSearchIssues(ctx, req.JQL, maxResults)
}

func (ts *toolSet) jiraGetIssue(ctx context.Context, req jiraGetIssueRequest) (*IssueDetail, error) {
	return ts.s.JiraGetIssue(ctx, req.IssueKey)
}

func (ts *toolSet) jiraCreateIssue(ctx context.Context, req CreateIssueInput) (*IssueSummary, error) {
	return ts.s.JiraCreateIssue(ctx, req)
}

func (ts *toolSet) jiraUpdateIssue(ctx context.Context, req jiraUpdateIssueRequest) (string, error) {
	err := ts.s.JiraUpdateIssue(ctx, req.IssueKey, UpdateIssueInput{
		Summary:     req.Summary,
		Description: req.Description,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Issue %s updated successfully", req.IssueKey), nil
}

func (ts *toolSet) jiraAddComment(ctx context.Context, req jiraAddCommentRequest) (string, error) {
	err := ts.s.JiraAddComment(ctx, req.IssueKey, req.Body)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Comment added to %s", req.IssueKey), nil
}

func (ts *toolSet) jiraListTransitions(ctx context.Context, req jiraGetIssueRequest) ([]Transition, error) {
	return ts.s.JiraListTransitions(ctx, req.IssueKey)
}

func (ts *toolSet) jiraTransitionIssue(ctx context.Context, req jiraTransitionRequest) (string, error) {
	err := ts.s.JiraTransitionIssue(ctx, req.IssueKey, req.TransitionID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Issue %s transitioned successfully", req.IssueKey), nil
}

func (ts *toolSet) confluenceSearch(ctx context.Context, req confluenceSearchRequest) ([]PageSummary, error) {
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}
	return ts.s.ConfluenceSearch(ctx, req.CQL, maxResults)
}

func (ts *toolSet) confluenceGetPage(ctx context.Context, req confluenceGetPageRequest) (*PageDetail, error) {
	return ts.s.ConfluenceGetPage(ctx, req.PageID)
}

func (ts *toolSet) confluenceCreatePage(ctx context.Context, req CreatePageInput) (*PageSummary, error) {
	return ts.s.ConfluenceCreatePage(ctx, req)
}

func (ts *toolSet) confluenceUpdatePage(ctx context.Context, req confluenceUpdatePageRequest) (string, error) {
	err := ts.s.ConfluenceUpdatePage(ctx, req.PageID, UpdatePageInput{
		Title:   req.Title,
		Body:    req.Body,
		Version: req.Version,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Page %s updated successfully", req.PageID), nil
}
