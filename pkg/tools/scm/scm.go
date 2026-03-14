// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm

import (
	"context"
	"fmt"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the SCM provider.
//
//counterfeiter:generate . Service
type Service interface {
	Provider() string

	ListRepos(ctx context.Context, opts go_scm.ListOptions) ([]*go_scm.Repository, error)
	// FindRepo returns a single repository by name (e.g. owner/repo). Used for data ingestion to get description, link, language.
	FindRepo(ctx context.Context, repo string) (*go_scm.Repository, error)
	ListPullRequests(ctx context.Context, repo string, opts go_scm.PullRequestListOptions) ([]*go_scm.PullRequest, error)
	// ListIssues returns open (or closed) issues for the given repo. Used by the datasource to surface issue context.
	ListIssues(ctx context.Context, repo string, opts go_scm.IssueListOptions) ([]*go_scm.Issue, error)
	// ListCommits returns the most-recent commits for the given repo. Used by the datasource to extract recent authors.
	ListCommits(ctx context.Context, repo string, opts go_scm.CommitListOptions) ([]*go_scm.Commit, error)
	GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error)
	CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error)
	ListPullRequestChanges(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Change, error)
	ListPullRequestComments(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Comment, error)
	CreatePullRequestComment(ctx context.Context, repo string, number int, input *go_scm.CommentInput) (*go_scm.Comment, error)
	ListPullRequestCommits(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Commit, error)
	MergePullRequest(ctx context.Context, repo string, number int) error
	CreateOrUpdateFile(ctx context.Context, repo, path string, params *go_scm.ContentParams) error
	FindBranch(ctx context.Context, repo, name string) (*go_scm.Reference, error)
	CreateBranch(ctx context.Context, repo string, params *go_scm.ReferenceInput) error

	// Tool-facing methods: these accept a single request struct and return
	// tool-friendly responses so that each NewXTool constructor can simply
	// pass the method reference to NewFunctionTool.
	ListReposTool(ctx context.Context, req go_scm.ListOptions) (ListReposResponse, error)
	ListPullRequestsTool(ctx context.Context, req ListPullRequestsRequest) ([]PullRequestSummary, error)
	GetPullRequestTool(ctx context.Context, req GetPullRequestRequest) (*go_scm.PullRequest, error)
	CreatePullRequestTool(ctx context.Context, req CreatePullRequestRequest) (*go_scm.PullRequest, error)
	ListPRChangesTool(ctx context.Context, req PRNumberRequest) ([]ChangeSummary, error)
	ListPRCommentsTool(ctx context.Context, req PRNumberRequest) ([]CommentSummary, error)
	CreatePRCommentTool(ctx context.Context, req CreatePRCommentRequest) (*go_scm.Comment, error)
	ListPRCommitsTool(ctx context.Context, req PRNumberRequest) ([]CommitSummary, error)
	MergePRTool(ctx context.Context, req PRNumberRequest) (string, error)
	GetRepoContent(ctx context.Context, req GetRepoContentRequest) (*go_scm.Content, error)
	CommitAndPRTool(ctx context.Context, req CommitAndPRRequest) (CommitAndPRResponse, error)

	// Validate performs a lightweight health check to verify that the
	// provider's token is valid and the endpoint is reachable.
	Validate(ctx context.Context) error
}

// Config holds configuration for SCM providers
type Config struct {
	Provider string `json:"provider" yaml:"Provider,omitempty" toml:"Provider,omitempty"` // github, gitlab, etc.
	Token    string `json:"token" yaml:"Token,omitempty" toml:"Token,omitempty"`
	BaseURL  string `json:"base_url" yaml:"BaseURL,omitempty" toml:"BaseURL,omitempty"` // for enterprise instances

	DoNotLearn bool `json:"do_not_learn" yaml:"DoNotLearn,omitempty" toml:"DoNotLearn,omitempty"`
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
	wrapper := &scmWrapper{
		client:   client,
		provider: cfg.Provider,
	}
	if !cfg.DoNotLearn {
		datasource.RegisterConnectorFactory(cfg.Provider, func(ctx context.Context, opts datasource.ConnectorOptions) datasource.DataSource {
			return NewSCMConnector(wrapper)
		})
	}

	return wrapper, nil
}

func (s *scmWrapper) Provider() string {
	return s.provider
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

// ── Request / Response Types ────────────────────────────────────────────

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

type GetRepoContentRequest struct {
	Repo string `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	Path string `json:"path" jsonschema:"description=Path to the file,required"`
	Ref  string `json:"ref" jsonschema:"description=Branch,tag, or commit SHA,required"`
}

// FileChange describes a single file to create or update.
type FileChange struct {
	Path    string `json:"path" jsonschema:"description=File path in the repository (e.g. pkg/main.go),required"`
	Content string `json:"content" jsonschema:"description=Full file content as plain text,required"`
}

// CommitAndPRRequest is the input for the uber commit-and-PR tool.
type CommitAndPRRequest struct {
	Repo          string       `json:"repo" jsonschema:"description=Repository name (e.g. owner/name),required"`
	Branch        string       `json:"branch" jsonschema:"description=Target branch for commits,required"`
	BaseBranch    string       `json:"base_branch" jsonschema:"description=Base branch to create the target branch from (default: main)"`
	CommitMessage string       `json:"commit_message" jsonschema:"description=Commit message for the file changes,required"`
	Files         []FileChange `json:"files" jsonschema:"description=List of files to create or update,required"`
	CreatePR      bool         `json:"create_pr" jsonschema:"description=If true also open a Pull Request against the base branch"`
	PRTitle       string       `json:"pr_title" jsonschema:"description=PR title (required when create_pr is true)"`
	PRBody        string       `json:"pr_body" jsonschema:"description=PR description"`
}

// CommitAndPRResponse is the output of the uber commit-and-PR tool.
type CommitAndPRResponse struct {
	CommittedFiles []string `json:"committed_files"`
	Branch         string   `json:"branch"`
	PRNumber       int      `json:"pr_number,omitempty"`
	PRLink         string   `json:"pr_link,omitempty"`
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

func NewListReposTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.ListReposTool,
		function.WithName("scm_list_repos"),
		function.WithDescription("List repositories accessible to the current user."),
	)
}

func NewListPullRequestsTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.ListPullRequestsTool,
		function.WithName("scm_list_prs"),
		function.WithDescription("List pull requests for a repository. Returns open PRs by default."),
	)
}

func NewGetPullRequestTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.GetPullRequestTool,
		function.WithName("scm_get_pr"),
		function.WithDescription("Get details of a specific Pull Request."),
	)
}

func NewCreatePullRequestTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.CreatePullRequestTool,
		function.WithName("scm_create_pr"),
		function.WithDescription("Create a new Pull Request from an existing branch that already has commits. Do not use this to make code changes; use scm_commit_and_pr instead if you need to modify files and open a PR."),
	)
}

func NewListPRChangesTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.ListPRChangesTool,
		function.WithName("scm_list_pr_changes"),
		function.WithDescription("List files changed in a Pull Request."),
	)
}

func NewListPRCommentsTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.ListPRCommentsTool,
		function.WithName("scm_list_pr_comments"),
		function.WithDescription("List comments on a Pull Request."),
	)
}

func NewCreatePRCommentTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.CreatePRCommentTool,
		function.WithName("scm_create_pr_comment"),
		function.WithDescription("Add a comment to a Pull Request."),
	)
}

func NewListPRCommitsTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.ListPRCommitsTool,
		function.WithName("scm_list_pr_commits"),
		function.WithDescription("List commits in a Pull Request."),
	)
}

func NewMergePRTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.MergePRTool,
		function.WithName("scm_merge_pr"),
		function.WithDescription("Merge a Pull Request."),
	)
}

// NewGetRepoContentTool creates a tool that retrieves the content of a file in a repository.
func NewGetRepoContentTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.GetRepoContent,
		function.WithName("scm_get_repo_content"),
		function.WithDescription("Get the content of a file in a repository."),
	)
}

// NewCommitAndPRTool creates a tool that commits multiple file changes to a branch
// and optionally opens a Pull Request.
func NewCommitAndPRTool(s Service) tool.CallableTool {
	return function.NewFunctionTool(
		s.CommitAndPRTool,
		function.WithName("scm_commit_and_pr"),
		function.WithDescription("Create or update multiple files in a branch, commit them, and optionally open a Pull Request. Use this tool when you need to write code changes to the repository. Each file produces a separate commit."),
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
		NewGetRepoContentTool(s),
		NewCommitAndPRTool(s),
	}
}
