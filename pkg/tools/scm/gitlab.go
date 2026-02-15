package scm

import (
	"context"
	"fmt"
	"net/http"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/gitlab"
	"github.com/drone/go-scm/scm/transport/oauth2"
)

const defaultGitLabURL = "https://gitlab.com"

// gitlabService implements the Service interface using the GitLab API.
// It wraps a go-scm Client configured with the GitLab driver, providing
// a uniform interface for repository and merge-request operations.
// Without this adapter the SCM tool layer would need GitLab-specific
// HTTP logic scattered across the codebase.
type gitlabService struct {
	client *go_scm.Client
}

// newGitLab creates a gitlabService from the given Config.
// If cfg.BaseURL is empty the public GitLab instance is used.
// If cfg.Token is empty an error is returned because all
// GitLab API operations require authentication.
func newGitLab(cfg Config) (Service, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("gitlab: token is required")
	}

	base := cfg.BaseURL
	if base == "" {
		base = defaultGitLabURL
	}

	client, err := gitlab.New(base)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to create client: %w", err)
	}

	client.Client = &http.Client{
		Transport: &oauth2.Transport{
			Source: oauth2.StaticTokenSource(
				&go_scm.Token{Token: cfg.Token},
			),
		},
	}

	return &gitlabService{client: client}, nil
}

// ListRepos returns all repositories accessible to the authenticated user.
// It paginates through the GitLab API until all pages are consumed.
func (s *gitlabService) ListRepos(ctx context.Context) ([]*go_scm.Repository, error) {
	var all []*go_scm.Repository
	opts := go_scm.ListOptions{Page: 1, Size: 100}

	for {
		repos, res, err := s.client.Repositories.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("gitlab: failed to list repos: %w", err)
		}
		all = append(all, repos...)

		if res.Page.Next == 0 {
			break
		}
		opts.Page = res.Page.Next
	}

	return all, nil
}

// GetPullRequest returns a single merge request by repository slug and number.
func (s *gitlabService) GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Find(ctx, repo, id)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to get MR %d in %s: %w", id, repo, err)
	}
	return pr, nil
}

// CreatePullRequest opens a new merge request in the given repository.
func (s *gitlabService) CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Create(ctx, repo, input)
	if err != nil {
		return nil, fmt.Errorf("gitlab: failed to create MR in %s: %w", repo, err)
	}
	return pr, nil
}
