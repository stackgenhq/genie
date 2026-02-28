package scm

import (
	"context"
	"fmt"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/traverse"
)

// scmWrapper implements the Service interface by wrapping a go-scm Client.
// It provides a uniform interface for repository and pull-request operations
// across all supported SCM providers (GitHub, GitLab, Bitbucket).
type scmWrapper struct {
	client *go_scm.Client
}

// ListRepos returns all repositories accessible to the authenticated user.
// It paginates through the GitHub API until all pages are consumed.
func (s *scmWrapper) ListRepos(ctx context.Context, request go_scm.ListOptions) ([]*go_scm.Repository, error) {
	repos, err := traverse.ReposV2(ctx, s.client, request)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list repos: %w", err)
	}
	return repos, nil
}

// FindRepo returns a single repository by name (e.g. owner/repo). Used for data ingestion to get metadata (description, link, language).
func (s *scmWrapper) FindRepo(ctx context.Context, repo string) (*go_scm.Repository, error) {
	r, _, err := s.client.Repositories.Find(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to find repo %s: %w", repo, err)
	}
	return r, nil
}

// ListPullRequests returns pull requests for the given repository.
func (s *scmWrapper) ListPullRequests(ctx context.Context, repo string, opts go_scm.PullRequestListOptions) ([]*go_scm.PullRequest, error) {
	prs, _, err := s.client.PullRequests.List(ctx, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PRs in %s: %w", repo, err)
	}
	return prs, nil
}

// GetPullRequest returns a single pull request by repository slug and number.
func (s *scmWrapper) GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Find(ctx, repo, id)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to get PR %d in %s: %w", id, repo, err)
	}
	return pr, nil
}

// CreatePullRequest opens a new pull request in the given repository.
func (s *scmWrapper) CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Create(ctx, repo, input)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to create PR in %s: %w", repo, err)
	}
	return pr, nil
}

// ListPullRequestChanges returns the files changed in a pull request.
func (s *scmWrapper) ListPullRequestChanges(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Change, error) {
	changes, _, err := s.client.PullRequests.ListChanges(ctx, repo, number, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PR changes for #%d in %s: %w", number, repo, err)
	}
	return changes, nil
}

// ListPullRequestComments returns comments on a pull request.
func (s *scmWrapper) ListPullRequestComments(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Comment, error) {
	comments, _, err := s.client.PullRequests.ListComments(ctx, repo, number, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PR comments for #%d in %s: %w", number, repo, err)
	}
	return comments, nil
}

// CreatePullRequestComment adds a comment to a pull request.
func (s *scmWrapper) CreatePullRequestComment(ctx context.Context, repo string, number int, input *go_scm.CommentInput) (*go_scm.Comment, error) {
	comment, _, err := s.client.PullRequests.CreateComment(ctx, repo, number, input)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to create PR comment on #%d in %s: %w", number, repo, err)
	}
	return comment, nil
}

// ListPullRequestCommits returns the commits in a pull request.
func (s *scmWrapper) ListPullRequestCommits(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Commit, error) {
	commits, _, err := s.client.PullRequests.ListCommits(ctx, repo, number, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PR commits for #%d in %s: %w", number, repo, err)
	}
	return commits, nil
}

// MergePullRequest merges a pull request.
func (s *scmWrapper) MergePullRequest(ctx context.Context, repo string, number int) error {
	_, err := s.client.PullRequests.Merge(ctx, repo, number)
	if err != nil {
		return fmt.Errorf("scm: failed to merge PR #%d in %s: %w", number, repo, err)
	}
	return nil
}

// Validate performs a lightweight health check by listing repos (page 1, size 1)
// to verify the token and endpoint are valid.
func (s *scmWrapper) Validate(ctx context.Context) error {
	_, _, err := s.client.Repositories.List(ctx, go_scm.ListOptions{Page: 1, Size: 1})
	if err != nil {
		return fmt.Errorf("scm: validate failed: %w", err)
	}
	return nil
}
