package websearch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/duckduckgo"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
	"trpc.group/trpc-go/trpc-agent-go/tool/google/search"
)

// SearchRequest is the input for the web_search tool.
type SearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to execute"`
}

// Config holds configuration for the web search tool.
type Config struct {
	GoogleAPIKey string `yaml:"google_api_key" toml:"google_api_key"`
	GoogleCX     string `yaml:"google_cx" toml:"google_cx"`
}

// unifiedSearchTool implements the web_search tool.
type unifiedSearchTool struct {
	cfg     Config
	ddgTool tool.Tool
	google  tool.Tool
}

// NewTool creates a new unified web search tool.
// It prioritizes Google Custom Search if API keys are available in the configuration.
// Otherwise, it falls back to DuckDuckGo.
func NewTool(cfg Config) tool.CallableTool {
	u := &unifiedSearchTool{
		cfg: cfg,
	}

	// Initialize DuckDuckGo as fallback
	u.ddgTool = duckduckgo.NewTool()

	// Initialize Google if creds are present
	if u.cfg.GoogleAPIKey != "" && u.cfg.GoogleCX != "" {
		gToolSet, err := search.NewToolSet(
			context.Background(),
			search.WithEngineID(u.cfg.GoogleCX),
			search.WithAPIKey(u.cfg.GoogleAPIKey),
		)
		if err == nil {
			tools := gToolSet.Tools(context.Background())
			if len(tools) > 0 {
				u.google = tools[0]
			}
		}
	}

	return function.NewFunctionTool(
		u.Search,
		function.WithName("web_search"),
		function.WithDescription("Search the web for information. Useful for finding documentation, libraries, or solving errors."),
	)
}

// Search executes the search query using the configured providers.
func (u *unifiedSearchTool) Search(ctx context.Context, req SearchRequest) (string, error) {
	log := logger.GetLogger(ctx)

	// Try Google First
	if u.google != nil {
		log.Info("Attempting search with Google", "query", req.Query)

		googleInput := map[string]interface{}{
			"query": req.Query,
			"size":  5, // Default size
		}
		inputBytes, _ := json.Marshal(googleInput)

		// tool.Call expects []byte and returns (any, error)
		if callable, ok := u.google.(tool.CallableTool); ok {
			res, err := callable.Call(ctx, inputBytes)
			if err == nil {
				// Result might be a struct or map or string depending on tool implementation.
				// Search tool returns `search.result` struct usually.
				// We need to marshal it back to string or format it.
				// Let's rely on fmt.Sprint or json marshal for now.
				resBytes, err := json.Marshal(res)
				if err != nil {
					return "", fmt.Errorf("failed to marshal Google search result: %w", err)
				}
				return string(resBytes), nil
			}
			log.Warn("Google search failed, falling back to DDG", "error", err)
		}
	}

	// Fallback to DuckDuckGo
	log.Info("Attempting search with DuckDuckGo", "query", req.Query)
	ddgInput := map[string]string{
		"query": req.Query,
	}
	inputBytes, _ := json.Marshal(ddgInput)

	if callable, ok := u.ddgTool.(tool.CallableTool); ok {
		res, err := callable.Call(ctx, inputBytes)
		if err != nil {
			return "", fmt.Errorf("all search providers failed. DDG error: %w", err)
		}
		return fmt.Sprintf("[Source: DuckDuckGo]\n%v", res), nil
	}

	return "", fmt.Errorf("internal error: tools are not callable")
}
