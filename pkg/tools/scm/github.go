package scm

import (
	"context"
	"fmt"
	"net/http"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/github"
	"github.com/drone/go-scm/scm/transport/oauth2"
)

const defaultGitHubURL = "https://api.github.com"

// githubService implements the Service interface using the GitHub API.
// It wraps a go-scm Client configured with the GitHub driver, providing
// a uniform interface for repository and pull-request operations.
// Without this adapter the SCM tool layer would need GitHub-specific
// HTTP logic scattered across the codebase.
type githubService struct {
	client *go_scm.Client
}

// newGitHub creates a githubService from the given Config.
// If cfg.BaseURL is empty the public GitHub API is used.
// If cfg.Token is empty an error is returned because all
// GitHub API operations require authentication.
func newGitHub(cfg Config) (Service, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("github: token is required")
	}

	base := cfg.BaseURL
	if base == "" {
		base = defaultGitHubURL
	}

	client, err := github.New(base)
	if err != nil {
		return nil, fmt.Errorf("github: failed to create client: %w", err)
	}

	client.Client = &http.Client{
		Transport: &oauth2.Transport{
			Source: oauth2.StaticTokenSource(
				&go_scm.Token{Token: cfg.Token},
			),
		},
	}

	return &githubService{client: client}, nil
}

// ListRepos returns all repositories accessible to the authenticated user.
// It paginates through the GitHub API until all pages are consumed.
func (s *githubService) ListRepos(ctx context.Context) ([]*go_scm.Repository, error) {
	var all []*go_scm.Repository
	opts := go_scm.ListOptions{Page: 1, Size: 100}

	for {
		repos, res, err := s.client.Repositories.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("github: failed to list repos: %w", err)
		}
		all = append(all, repos...)

		if res.Page.Next == 0 {
			break
		}
		opts.Page = res.Page.Next
	}

	return all, nil
}

// GetPullRequest returns a single pull request by repository slug and number.
func (s *githubService) GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Find(ctx, repo, id)
	if err != nil {
		return nil, fmt.Errorf("github: failed to get PR %d in %s: %w", id, repo, err)
	}
	return pr, nil
}

// CreatePullRequest opens a new pull request in the given repository.
func (s *githubService) CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Create(ctx, repo, input)
	if err != nil {
		return nil, fmt.Errorf("github: failed to create PR in %s: %w", repo, err)
	}
	return pr, nil
}
