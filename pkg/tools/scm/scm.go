package scm

import (
	"context"
	"fmt"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the SCM provider.
//
//counterfeiter:generate . Service
type Service interface {
	ListRepos(ctx context.Context, opts go_scm.ListOptions) ([]*go_scm.Repository, error)
	// FindRepo returns a single repository by name (e.g. owner/repo). Used for data ingestion to get description, link, language.
	FindRepo(ctx context.Context, repo string) (*go_scm.Repository, error)
	ListPullRequests(ctx context.Context, repo string, opts go_scm.PullRequestListOptions) ([]*go_scm.PullRequest, error)
	GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error)
	CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error)
	ListPullRequestChanges(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Change, error)
	ListPullRequestComments(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Comment, error)
	CreatePullRequestComment(ctx context.Context, repo string, number int, input *go_scm.CommentInput) (*go_scm.Comment, error)
	ListPullRequestCommits(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Commit, error)
	MergePullRequest(ctx context.Context, repo string, number int) error

	// Validate performs a lightweight health check to verify that the
	// provider's token is valid and the endpoint is reachable.
	Validate(ctx context.Context) error
}

// Config holds configuration for SCM providers
type Config struct {
	Provider string `json:"provider" yaml:"Provider,omitempty" toml:"Provider,omitempty"` // github, gitlab, etc.
	Token    string `json:"token" yaml:"Token,omitempty" toml:"Token,omitempty"`
	BaseURL  string `json:"base_url" yaml:"BaseURL,omitempty" toml:"BaseURL,omitempty"` // for enterprise instances
}

// New creates a new SCM Service based on the configuration.
// It dispatches to the appropriate provider constructor based on cfg.Provider.
// Without this factory, callers would need to know about each provider's
// internal constructor, coupling them to implementation details.
func New(cfg Config) (Service, error) {
	logger.GetLogger(context.Background()).Info("Initializing SCM service", "provider", cfg.Provider, "has_token", cfg.Token != "")
	client, err := cfg.getClient()
	if err != nil {
		return nil, err
	}
	return &scmWrapper{
		client: client,
	}, nil
}

func (cfg Config) getClient() (*go_scm.Client, error) {
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

type ListPullRequestsRequest struct {
	Repo  string `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	State string `json:"state" jsonschema:"description=Filter by state: open (default) or closed"`
}

type PRNumberRequest struct {
	Repo   string `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	Number int    `json:"number" jsonschema:"description=Pull Request number,required"`
}

type CreatePRCommentRequest struct {
	Repo   string `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	Number int    `json:"number" jsonschema:"description=Pull Request number,required"`
	Body   string `json:"body" jsonschema:"description=Comment body text,required"`
}

// PullRequestSummary is a simplified PR response for listing.
type PullRequestSummary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Source string `json:"source"`
	Target string `json:"target"`
	Author string `json:"author"`
}

// ChangeSummary is a simplified file-change response.
type ChangeSummary struct {
	Path    string `json:"path"`
	Added   bool   `json:"added,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	Renamed bool   `json:"renamed,omitempty"`
}

// CommentSummary is a simplified comment response.
type CommentSummary struct {
	ID     int    `json:"id"`
	Body   string `json:"body"`
	Author string `json:"author"`
}

// CommitSummary is a simplified commit response.
type CommitSummary struct {
	Sha     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
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

func NewListPullRequestsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listPullRequests,
		function.WithName("scm_list_prs"),
		function.WithDescription("List pull requests for a repository. Returns open PRs by default."),
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

func NewListPRChangesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listPRChanges,
		function.WithName("scm_list_pr_changes"),
		function.WithDescription("List files changed in a Pull Request."),
	)
}

func NewListPRCommentsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listPRComments,
		function.WithName("scm_list_pr_comments"),
		function.WithDescription("List comments on a Pull Request."),
	)
}

func NewCreatePRCommentTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createPRComment,
		function.WithName("scm_create_pr_comment"),
		function.WithDescription("Add a comment to a Pull Request."),
	)
}

func NewListPRCommitsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listPRCommits,
		function.WithName("scm_list_pr_commits"),
		function.WithDescription("List commits in a Pull Request."),
	)
}

func NewMergePRTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.mergePR,
		function.WithName("scm_merge_pr"),
		function.WithDescription("Merge a Pull Request."),
	)
}

func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewListReposTool(s),
		NewListPullRequestsTool(s),
		NewGetPullRequestTool(s),
		NewCreatePullRequestTool(s),
		NewListPRChangesTool(s),
		NewListPRCommentsTool(s),
		NewCreatePRCommentTool(s),
		NewListPRCommitsTool(s),
		NewMergePRTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) listRepos(ctx context.Context, listReposRequest go_scm.ListOptions) (ListReposResponse, error) {
	repos, err := ts.s.ListRepos(ctx, listReposRequest)
	if err != nil {
		return ListReposResponse{}, err
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		// Return full namespace/name (e.g. "stackgenhq/genie") so the
		// agent can pass it directly to scm_list_prs without guessing
		// the org/owner prefix.
		if r.Namespace != "" {
			names[i] = r.Namespace + "/" + r.Name
		} else {
			names[i] = r.Name
		}
	}
	return ListReposResponse{Repositories: names}, nil
}

func (ts *toolSet) listPullRequests(ctx context.Context, req ListPullRequestsRequest) ([]PullRequestSummary, error) {
	opts := go_scm.PullRequestListOptions{
		Page:   1,
		Size:   50,
		Open:   true,
		Closed: false,
	}
	if req.State == "closed" {
		opts.Open = false
		opts.Closed = true
	}

	prs, err := ts.s.ListPullRequests(ctx, req.Repo, opts)
	if err != nil {
		return nil, err
	}

	summaries := make([]PullRequestSummary, 0, len(prs))
	for _, pr := range prs {
		author := ""
		if pr.Author.Login != "" {
			author = pr.Author.Login
		}
		state := "open"
		if pr.Closed {
			state = "closed"
		}
		if pr.Merged {
			state = "merged"
		}
		summaries = append(summaries, PullRequestSummary{
			Number: pr.Number,
			Title:  pr.Title,
			State:  state,
			Source: pr.Source,
			Target: pr.Target,
			Author: author,
		})
	}
	return summaries, nil
}

func (ts *toolSet) getPullRequest(ctx context.Context, req GetPullRequestRequest) (*go_scm.PullRequest, error) {
	return ts.s.GetPullRequest(ctx, req.Repo, req.ID)
}

func (ts *toolSet) createPullRequest(ctx context.Context, req CreatePullRequestRequest) (*go_scm.PullRequest, error) {
	input := &go_scm.PullRequestInput{
		Title:  req.Title,
		Body:   req.Body,
		Source: req.Head,
		Target: req.Base,
	}
	return ts.s.CreatePullRequest(ctx, req.Repo, input)
}

func (ts *toolSet) listPRChanges(ctx context.Context, req PRNumberRequest) ([]ChangeSummary, error) {
	changes, err := ts.s.ListPullRequestChanges(ctx, req.Repo, req.Number, go_scm.ListOptions{Page: 1, Size: 100})
	if err != nil {
		return nil, err
	}
	out := make([]ChangeSummary, 0, len(changes))
	for _, c := range changes {
		out = append(out, ChangeSummary{
			Path:    c.Path,
			Added:   c.Added,
			Deleted: c.Deleted,
			Renamed: c.Renamed,
		})
	}
	return out, nil
}

func (ts *toolSet) listPRComments(ctx context.Context, req PRNumberRequest) ([]CommentSummary, error) {
	comments, err := ts.s.ListPullRequestComments(ctx, req.Repo, req.Number, go_scm.ListOptions{Page: 1, Size: 100})
	if err != nil {
		return nil, err
	}
	out := make([]CommentSummary, 0, len(comments))
	for _, c := range comments {
		out = append(out, CommentSummary{
			ID:     c.ID,
			Body:   c.Body,
			Author: c.Author.Login,
		})
	}
	return out, nil
}

func (ts *toolSet) createPRComment(ctx context.Context, req CreatePRCommentRequest) (*go_scm.Comment, error) {
	return ts.s.CreatePullRequestComment(ctx, req.Repo, req.Number, &go_scm.CommentInput{Body: req.Body})
}

func (ts *toolSet) listPRCommits(ctx context.Context, req PRNumberRequest) ([]CommitSummary, error) {
	commits, err := ts.s.ListPullRequestCommits(ctx, req.Repo, req.Number, go_scm.ListOptions{Page: 1, Size: 100})
	if err != nil {
		return nil, err
	}
	out := make([]CommitSummary, 0, len(commits))
	for _, c := range commits {
		author := c.Author.Login
		if author == "" {
			author = c.Author.Name
		}
		out = append(out, CommitSummary{
			Sha:     c.Sha,
			Message: c.Message,
			Author:  author,
		})
	}
	return out, nil
}

func (ts *toolSet) mergePR(ctx context.Context, req PRNumberRequest) (string, error) {
	err := ts.s.MergePullRequest(ctx, req.Repo, req.Number)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("PR #%d merged successfully", req.Number), nil
}
