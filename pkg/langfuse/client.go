package langfuse

import (
	"context"
	"net/http"

	"github.com/appcd-dev/genie/pkg/ttlcache"
)

//go:generate go tool counterfeiter -generate

// Client defines the interface for fetching prompts from Langfuse.
//
//counterfeiter:generate . Client
type Client interface {
	// GetPrompt returns the prompt template by name, or the default if not found/disabled.
	GetPrompt(ctx context.Context, name, defaultPrompt string) string
}

type client struct {
	httpClient   *http.Client
	promptsCache *ttlcache.Item[remotePrompts]
	config       Config
}

var defaultClient Client

func GetPrompt(ctx context.Context, name, defaultPrompt string) string {
	if defaultClient == nil {
		return defaultPrompt
	}
	return defaultClient.GetPrompt(ctx, name, defaultPrompt)
}

type noopClient struct {
}

func (n *noopClient) GetPrompt(ctx context.Context, name, defaultPrompt string) string {
	return defaultPrompt
}
