package pm

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// Supported provider names.
const (
	ProviderJira   = "jira"
	ProviderLinear = "linear"
	ProviderAsana  = "asana"
)

// Service abstracts Issue Tracking Systems (JIRA, Linear, Asana).
type Service interface {
	GetIssue(ctx context.Context, id string) (*Issue, error)
	CreateIssue(ctx context.Context, input IssueInput) (*Issue, error)
	AssignIssue(ctx context.Context, id string, assignee string) error
}

type Issue struct {
	ID          string
	Title       string
	Description string
	Status      string
	Assignee    string
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

type CreateIssueRequest struct {
	Title       string `json:"title" jsonschema:"description=Issue Title,required"`
	Description string `json:"description" jsonschema:"description=Issue Description"`
	Project     string `json:"project" jsonschema:"description=Project Key/ID,required"`
	Type        string `json:"type" jsonschema:"description=Issue Type (Bug, Task, Story),required"`
}

type AssignIssueRequest struct {
	ID       string `json:"id" jsonschema:"description=Issue ID,required"`
	Assignee string `json:"assignee" jsonschema:"description=User to assign to,required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewGetIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getIssue,
		function.WithName("pm_get_issue"),
		function.WithDescription("Get details of a specific issue/ticket."),
	)
}

func NewCreateIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createIssue,
		function.WithName("pm_create_issue"),
		function.WithDescription("Create a new issue/ticket."),
	)
}

func NewAssignIssueTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.assignIssue,
		function.WithName("pm_assign_issue"),
		function.WithDescription("Assign an issue to a user."),
	)
}

func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewGetIssueTool(s),
		NewCreateIssueTool(s),
		NewAssignIssueTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) getIssue(ctx context.Context, req GetIssueRequest) (*Issue, error) {
	return ts.s.GetIssue(ctx, req.ID)
}

func (ts *toolSet) createIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	input := IssueInput{
		Title:       req.Title,
		Description: req.Description,
		Project:     req.Project,
		Type:        req.Type,
	}
	return ts.s.CreateIssue(ctx, input)
}

func (ts *toolSet) assignIssue(ctx context.Context, req AssignIssueRequest) (string, error) {
	err := ts.s.AssignIssue(ctx, req.ID, req.Assignee)
	if err != nil {
		return "", err
	}
	return "assigned", nil
}
