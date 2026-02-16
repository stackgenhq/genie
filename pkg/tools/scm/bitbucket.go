package scm

import (
	"fmt"
	"net/http"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/bitbucket"
	"github.com/drone/go-scm/scm/transport/oauth2"
)

const defaultBitbucketURL = "https://api.bitbucket.org"

// newBitbucket creates a go-scm Client from the given Config.
// If cfg.BaseURL is empty the public Bitbucket Cloud API is used.
// If cfg.Token is empty an error is returned because all
// Bitbucket API operations require authentication.
func newBitbucket(cfg Config) (*go_scm.Client, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("bitbucket: token is required")
	}

	base := cfg.BaseURL
	if base == "" {
		base = defaultBitbucketURL
	}

	client, err := bitbucket.New(base)
	if err != nil {
		return nil, fmt.Errorf("bitbucket: failed to create client: %w", err)
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
