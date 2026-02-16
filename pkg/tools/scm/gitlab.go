package scm

import (
	"fmt"
	"net/http"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/gitlab"
	"github.com/drone/go-scm/scm/transport/oauth2"
)

const defaultGitLabURL = "https://gitlab.com"

// newGitLab creates a gitlabService from the given Config.
// If cfg.BaseURL is empty the public GitLab instance is used.
// If cfg.Token is empty an error is returned because all
// GitLab API operations require authentication.
func newGitLab(cfg Config) (*go_scm.Client, error) {
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

	return client, nil
}
