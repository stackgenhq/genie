package langfuse

import (
	"context"
	"net/http"

	"github.com/stackgenhq/genie/pkg/ttlcache"
)

//go:generate go tool counterfeiter -generate

// Client defines the interface for interacting with the Langfuse API,
// including prompt management and usage metrics retrieval.
//
//counterfeiter:generate . Client
type Client interface {
	// GetPrompt returns the prompt template by name, or the default if not found/disabled.
	GetPrompt(ctx context.Context, name, defaultPrompt string) string

	// GetAgentStats returns aggregated token usage and cost statistics per
	// agent (trace name) for the duration specified in the request. Without
	// this method, consumers would need to call the Langfuse metrics API
	// directly, duplicating auth and parsing logic.
	GetAgentStats(ctx context.Context, req GetAgentStatsRequest) ([]AgentUsageStats, error)
}

type client struct {
	httpClient   *http.Client
	promptsCache *ttlcache.Item[remotePrompts]
	config       Config
}

var defaultClient Client

// GetPrompt delegates to the global defaultClient's GetPrompt. Returns
// defaultPrompt when no client is configured. Exists to provide a convenient
// package-level API without requiring callers to manage a Client instance.
func GetPrompt(ctx context.Context, name, defaultPrompt string) string {
	if defaultClient == nil {
		return defaultPrompt
	}
	return defaultClient.GetPrompt(ctx, name, defaultPrompt)
}

// GetAgentStats delegates to the global defaultClient's GetAgentStats. Returns
// nil, nil when no client is configured (callers should handle accordingly).
// Exists to allow callers to get per-agent usage stats without managing their
// own Client instance.
func GetAgentStats(ctx context.Context, req GetAgentStatsRequest) ([]AgentUsageStats, error) {
	if defaultClient == nil {
		return nil, nil
	}
	return defaultClient.GetAgentStats(ctx, req)
}

type noopClient struct {
}

func (n *noopClient) GetPrompt(_ context.Context, _, defaultPrompt string) string {
	return defaultPrompt
}

func (n *noopClient) GetAgentStats(_ context.Context, _ GetAgentStatsRequest) ([]AgentUsageStats, error) {
	return nil, nil
}
