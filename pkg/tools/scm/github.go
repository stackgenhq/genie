package scm

import (
	"fmt"
	"net/http"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/github"
	"github.com/drone/go-scm/scm/transport/oauth2"
)

const defaultGitHubURL = "https://api.github.com"

// newGitHub creates a githubService from the given Config.
// If cfg.BaseURL is empty the public GitHub API is used.
// If cfg.Token is empty an error is returned because all
// GitHub API operations require authentication.
func newGitHub(cfg Config) (*go_scm.Client, error) {
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

	return client, nil
}
