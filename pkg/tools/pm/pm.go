package pm

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Supported provider names.
const (
	ProviderJira   = "jira"
	ProviderLinear = "linear"
	ProviderAsana  = "asana"
)

// operation names for capability checking.
const (
	opGetIssue     = "get_issue"
	opListIssues   = "list_issues"
	opCreateIssue  = "create_issue"
	opAssignIssue  = "assign_issue"
	opUpdateIssue  = "update_issue"
	opAddComment   = "add_comment"
	opListComments = "list_comments"
	opSearchIssues = "search_issues"
	opListTeams    = "list_teams"
	opListLabels   = "list_labels"
	opAddLabel     = "add_label"
	opListUsers    = "list_users"
)

//counterfeiter:generate . Service

// Service abstracts Issue Tracking Systems (JIRA, Linear, Asana).
type Service interface {
	// Supported returns the list of operation names this provider implements.
	Supported() []string

	// Validate performs a lightweight health check to verify that the
	// provider's token is valid and the endpoint is reachable.
	Validate(ctx context.Context) error

	GetIssue(ctx context.Context, id string) (*Issue, error)
	ListIssues(ctx context.Context, filter IssueFilter) ([]*Issue, error)
	CreateIssue(ctx context.Context, input IssueInput) (*Issue, error)
	AssignIssue(ctx context.Context, id string, assignee string) error
	UpdateIssue(ctx context.Context, id string, update IssueUpdate) (*Issue, error)
	AddComment(ctx context.Context, issueID string, body string) (*Comment, error)
	ListComments(ctx context.Context, issueID string) ([]*Comment, error)
	SearchIssues(ctx context.Context, query string) ([]*Issue, error)
	ListTeams(ctx context.Context) ([]*Team, error)
	ListLabels(ctx context.Context, teamID string) ([]*Label, error)
	AddLabel(ctx context.Context, issueID string, labelID string) error
	ListUsers(ctx context.Context) ([]*User, error)
}

type Issue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Assignee    string   `json:"assignee,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type Comment struct {
	ID     string `json:"id"`
	Body   string `json:"body"`
	Author string `json:"author,omitempty"`
}

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// IssueFilter controls which issues are returned by ListIssues.
type IssueFilter struct {
	Status string // optional: filter by status (e.g. "open", "in_progress", "closed")
}

// IssueUpdate holds optional fields for updating an issue.
type IssueUpdate struct {
	Title       *string // nil = no change
	Description *string
	Status      *string // workflow state name (e.g. "In Progress", "Done")
}

type IssueInput struct {
	Title       string
	Description string
	Project     string
	Type        string
}

// Config holds configuration for PM providers.
type Config struct {
	Provider string `yaml:"provider" toml:"provider"` // jira, linear, asana
	APIToken string `yaml:"api_token" toml:"api_token"`
	BaseURL  string `yaml:"base_url" toml:"base_url"` // Jira: required; Linear/Asana: optional override
	Email    string `yaml:"email" toml:"email"`       // Jira only: email for Basic auth
}

// New creates a new PM Service based on the configuration.
func New(cfg Config) (Service, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderJira:
		return newJira(cfg)
	case ProviderLinear:
		return newLinear(cfg)
	case ProviderAsana:
		return newAsana(cfg)
	default:
		return nil, fmt.Errorf("pm: unsupported provider %q (supported: jira, linear, asana)", cfg.Provider)
	}
}

// ── Tool Definitions ────────────────────────────────────────────────────

type GetIssueRequest struct {
	ID string `json:"id" jsonschema:"description=Issue ID (e.g. PROJ-123),required"`
}

type ListIssuesRequest struct {
	Status string `json:"status" jsonschema:"description=Optional status filter: open or closed. Defaults to open."`
}

type CreateIssueRequest struct {
	Title       string `json:"title" jsonschema:"description=Issue Title,required"`
	Description string `json:"description" jsonschema:"description=Issue Description"`
	Project     string `json:"project" jsonschema:"description=Project Key/ID,required"`
	Type        string `json:"type" jsonschema:"description=Issue Type (Bug Task Story),required"`
}

type AssignIssueRequest struct {
	ID       string `json:"id" jsonschema:"description=Issue ID,required"`
	Assignee string `json:"assignee" jsonschema:"description=User to assign to,required"`
}

type UpdateIssueRequest struct {
	ID          string  `json:"id" jsonschema:"description=Issue ID,required"`
	Title       *string `json:"title" jsonschema:"description=New title (omit to keep current)"`
	Description *string `json:"description" jsonschema:"description=New description (omit to keep current)"`
	Status      *string `json:"status" jsonschema:"description=New status/state name e.g. In Progress or Done (omit to keep current)"`
}

type AddCommentRequest struct {
	IssueID string `json:"issue_id" jsonschema:"description=Issue ID to comment on,required"`
	Body    string `json:"body" jsonschema:"description=Comment body text,required"`
}

type ListCommentsRequest struct {
	IssueID string `json:"issue_id" jsonschema:"description=Issue ID to list comments for,required"`
}

type SearchIssuesRequest struct {
	Query string `json:"query" jsonschema:"description=Free-text search query,required"`
}

type ListTeamsRequest struct{}

type ListLabelsRequest struct {
	TeamID string `json:"team_id" jsonschema:"description=Team ID to list labels for (use pm_list_teams to discover),required"`
}

type AddLabelRequest struct {
	IssueID string `json:"issue_id" jsonschema:"description=Issue ID,required"`
	LabelID string `json:"label_id" jsonschema:"description=Label ID to add (use pm_list_labels to discover),required"`
}

type ListUsersRequest struct{}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewGetIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getIssue,
		function.WithName("projectmanagement_get_issue"),
		function.WithDescription("Get details of a specific issue/ticket."),
	)
}

func NewListIssuesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listIssues,
		function.WithName("projectmanagement_list_issues"),
		function.WithDescription("List issues/tickets. Returns open issues by default. Use status='closed' to see completed issues."),
	)
}

func NewCreateIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createIssue,
		function.WithName("projectmanagement_create_issue"),
		function.WithDescription("Create a new issue/ticket."),
	)
}

func NewAssignIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.assignIssue,
		function.WithName("projectmanagement_assign_issue"),
		function.WithDescription("Assign an issue to a user."),
	)
}

func NewUpdateIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.updateIssue,
		function.WithName("projectmanagement_update_issue"),
		function.WithDescription("Update an issue: change title, description, or transition to a new status/state."),
	)
}

func NewAddCommentTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.addComment,
		function.WithName("projectmanagement_add_comment"),
		function.WithDescription("Add a comment to an issue for audit trail or discussion."),
	)
}

func NewListCommentsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listComments,
		function.WithName("projectmanagement_list_comments"),
		function.WithDescription("List all comments on an issue."),
	)
}

func NewSearchIssuesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.searchIssues,
		function.WithName("projectmanagement_search_issues"),
		function.WithDescription("Full-text search across all issues."),
	)
}

func NewListTeamsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listTeams,
		function.WithName("projectmanagement_list_teams"),
		function.WithDescription("List all teams/projects. Useful for discovering team IDs when creating issues."),
	)
}

func NewListLabelsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listLabels,
		function.WithName("projectmanagement_list_labels"),
		function.WithDescription("List all labels available for a team. Use pm_list_teams to discover team IDs."),
	)
}

func NewAddLabelTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.addLabel,
		function.WithName("projectmanagement_add_label"),
		function.WithDescription("Add a label to an issue."),
	)
}

func NewListUsersTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listUsers,
		function.WithName("projectmanagement_list_users"),
		function.WithDescription("List all users in the workspace. Useful for finding user IDs to assign issues."),
	)
}

func AllTools(s Service) []tool.Tool {
	// Map operation names to tool constructors.
	reg := map[string]func(Service) tool.CallableTool{
		opGetIssue:     NewGetIssueTool,
		opListIssues:   NewListIssuesTool,
		opCreateIssue:  NewCreateIssueTool,
		opAssignIssue:  NewAssignIssueTool,
		opUpdateIssue:  NewUpdateIssueTool,
		opAddComment:   NewAddCommentTool,
		opListComments: NewListCommentsTool,
		opSearchIssues: NewSearchIssuesTool,
		opListTeams:    NewListTeamsTool,
		opListLabels:   NewListLabelsTool,
		opAddLabel:     NewAddLabelTool,
		opListUsers:    NewListUsersTool,
	}

	var tools []tool.Tool
	for _, op := range s.Supported() {
		if ctor, ok := reg[op]; ok {
			tools = append(tools, ctor(s))
		}
	}
	return tools
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) getIssue(ctx context.Context, req GetIssueRequest) (*Issue, error) {
	return ts.s.GetIssue(ctx, req.ID)
}

func (ts *toolSet) listIssues(ctx context.Context, req ListIssuesRequest) ([]*Issue, error) {
	return ts.s.ListIssues(ctx, IssueFilter(req))
}

func (ts *toolSet) createIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	input := IssueInput(req)
	return ts.s.CreateIssue(ctx, input)
}

func (ts *toolSet) assignIssue(ctx context.Context, req AssignIssueRequest) (string, error) {
	err := ts.s.AssignIssue(ctx, req.ID, req.Assignee)
	if err != nil {
		return "", err
	}
	return "assigned", nil
}

func (ts *toolSet) updateIssue(ctx context.Context, req UpdateIssueRequest) (*Issue, error) {
	return ts.s.UpdateIssue(ctx, req.ID, IssueUpdate{
		Title: req.Title, Description: req.Description, Status: req.Status,
	})
}

func (ts *toolSet) addComment(ctx context.Context, req AddCommentRequest) (*Comment, error) {
	return ts.s.AddComment(ctx, req.IssueID, req.Body)
}

func (ts *toolSet) listComments(ctx context.Context, req ListCommentsRequest) ([]*Comment, error) {
	return ts.s.ListComments(ctx, req.IssueID)
}

func (ts *toolSet) searchIssues(ctx context.Context, req SearchIssuesRequest) ([]*Issue, error) {
	return ts.s.SearchIssues(ctx, req.Query)
}

func (ts *toolSet) listTeams(ctx context.Context, _ ListTeamsRequest) ([]*Team, error) {
	return ts.s.ListTeams(ctx)
}

func (ts *toolSet) listLabels(ctx context.Context, req ListLabelsRequest) ([]*Label, error) {
	return ts.s.ListLabels(ctx, req.TeamID)
}

func (ts *toolSet) addLabel(ctx context.Context, req AddLabelRequest) (string, error) {
	err := ts.s.AddLabel(ctx, req.IssueID, req.LabelID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("label %s added to %s", req.LabelID, req.IssueID), nil
}

func (ts *toolSet) listUsers(ctx context.Context, _ ListUsersRequest) ([]*User, error) {
	return ts.s.ListUsers(ctx)
}
