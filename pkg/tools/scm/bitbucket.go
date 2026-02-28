package scm

import (
	"fmt"
	"net/http"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/bitbucket"
	"github.com/stackgenhq/genie/pkg/httputil"
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

	client.Client = httputil.GetClient(func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	})

	return client, nil
}
