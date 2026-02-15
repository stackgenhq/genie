package scm

import (
	"context"
	"fmt"

	"github.com/drone/go-scm/scm"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// Service defines the capabilities of the SCM provider.
type Service interface {
	ListRepos(ctx context.Context) ([]*scm.Repository, error)
	GetPullRequest(ctx context.Context, repo string, id int) (*scm.PullRequest, error)
	CreatePullRequest(ctx context.Context, repo string, input *scm.PullRequestInput) (*scm.PullRequest, error)
}

// Config holds configuration for SCM providers
type Config struct {
	Provider string // github, gitlab, etc.
	Token    string
	BaseURL  string // for enterprise instances
}

// New creates a new SCM Service based on the configuration.
// It dispatches to the appropriate provider constructor based on cfg.Provider.
// Without this factory, callers would need to know about each provider's
// internal constructor, coupling them to implementation details.
func New(cfg Config) (Service, error) {
	switch cfg.Provider {
	case "github":
		return newGitHub(cfg)
	case "gitlab":
		return newGitLab(cfg)
	case "bitbucket":
		return newBitbucket(cfg)
	default:
		return nil, fmt.Errorf("unsupported SCM provider: %q", cfg.Provider)
	}
}

// ── Tool Definitions ────────────────────────────────────────────────────

type ListReposResponse struct {
	Repositories []string `json:"repositories"`
}

type GetPullRequestRequest struct {
	Repo string `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	ID   int    `json:"id" jsonschema:"description=Pull Request ID,required"`
}

type CreatePullRequestRequest struct {
	Repo  string `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	Title string `json:"title" jsonschema:"description=PR Title,required"`
	Body  string `json:"body" jsonschema:"description=PR Description"`
	Head  string `json:"head" jsonschema:"description=Source branch,required"`
	Base  string `json:"base" jsonschema:"description=Target branch,required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewListReposTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listRepos,
		function.WithName("scm_list_repos"),
		function.WithDescription("List repositories accessible to the current user."),
	)
}

func NewGetPullRequestTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getPullRequest,
		function.WithName("scm_get_pr"),
		function.WithDescription("Get details of a specific Pull Request."),
	)
}

func NewCreatePullRequestTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createPullRequest,
		function.WithName("scm_create_pr"),
		function.WithDescription("Create a new Pull Request."),
	)
}

func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewListReposTool(s),
		NewGetPullRequestTool(s),
		NewCreatePullRequestTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) listRepos(ctx context.Context, _ struct{}) (ListReposResponse, error) {
	repos, err := ts.s.ListRepos(ctx)
	if err != nil {
		return ListReposResponse{}, err
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return ListReposResponse{Repositories: names}, nil
}

func (ts *toolSet) getPullRequest(ctx context.Context, req GetPullRequestRequest) (*scm.PullRequest, error) {
	return ts.s.GetPullRequest(ctx, req.Repo, req.ID)
}

func (ts *toolSet) createPullRequest(ctx context.Context, req CreatePullRequestRequest) (*scm.PullRequest, error) {
	input := &scm.PullRequestInput{
		Title:  req.Title,
		Body:   req.Body,
		Source: req.Head,
		Target: req.Base,
	}
	return ts.s.CreatePullRequest(ctx, req.Repo, input)
}
