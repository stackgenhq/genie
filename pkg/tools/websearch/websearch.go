package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
	"trpc.group/trpc-go/trpc-agent-go/tool/google/search"
)

// Supported provider names for the Provider config field.
const (
	ProviderDuckDuckGo = "duckduckgo"
	ProviderGoogle     = "google"
	ProviderBing       = "bing"
)

// SearchRequest is the input for the web_search tool.
type SearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to execute"`
}

// Config holds configuration for the web search tool.
// Provider selects the search backend: "google", "bing", or "duckduckgo" (default).
// Only one provider is active at a time.
type Config struct {
	Provider     string `yaml:"provider" toml:"provider"`
	GoogleAPIKey string `yaml:"google_api_key" toml:"google_api_key"`
	GoogleCX     string `yaml:"google_cx" toml:"google_cx"`
	BingAPIKey   string `yaml:"bing_api_key" toml:"bing_api_key"`
}

// searchTool implements the web_search tool with a configurable backend.
type searchTool struct {
	cfg      Config
	backend  tool.Tool // the active provider's tool (DDG, Google, or Bing)
	provider string    // normalised provider name
	ddgOpts  []DDGOption
}

// NewTool creates a new web search tool configured to use a single provider.
// When Provider is empty it defaults to DuckDuckGo which requires no API keys.
// If the selected provider is missing credentials, it logs a warning and falls back to DuckDuckGo.
//
// Optional DDGOptions can be provided to configure the fallback DuckDuckGo tool (e.g. for testing).
func NewTool(cfg Config, opts ...DDGOption) tool.CallableTool {
	s := &searchTool{
		cfg:      cfg,
		provider: normaliseProvider(cfg.Provider),
		ddgOpts:  opts,
	}

	// Try to initialise the selected provider; fall back to DuckDuckGo
	// when credentials are missing or initialisation fails.
	switch s.provider {
	case ProviderGoogle:
		if cfg.GoogleAPIKey != "" && cfg.GoogleCX != "" {
			s.backend = initGoogle(cfg)
		}
	case ProviderBing:
		if cfg.BingAPIKey != "" {
			s.backend = NewBingTool(cfg.BingAPIKey)
		}
	}
	if s.backend == nil {
		s.provider = ProviderDuckDuckGo
		s.backend = NewDDGTool(opts...)
	}

	return function.NewFunctionTool(
		s.Search,
		function.WithName("web_search"),
		function.WithDescription("Search the web for information. Useful for finding documentation, libraries, or solving errors."),
	)
}

// Search executes the search query using the configured provider.
// If the primary provider fails, it automatically falls back to DuckDuckGo.
func (s *searchTool) Search(ctx context.Context, req SearchRequest) (string, error) {
	log := logger.GetLogger(ctx)

	var result string
	var err error

	// primary attempt
	switch s.provider {
	case ProviderGoogle:
		result, err = s.searchGoogle(ctx, req, log)
	case ProviderBing:
		result, err = s.searchBackend(ctx, req, log, "Bing")
	default:
		return s.searchBackend(ctx, req, log, "DuckDuckGo")
	}

	// success? return
	if err == nil {
		return result, nil
	}

	// failure? log and fallback to DDG (unless we were already using DDG)
	log.Warn("web_search: primary provider failed, falling back to duckduckgo",
		"provider", s.provider,
		"error", err,
	)

	// Fallback to DuckDuckGo
	// We instantiate a fresh DDG tool for the fallback to ensure clean state
	// but pass any original options (e.g. for testing)
	return s.fallbackSearchDDG(ctx, req, log, s.ddgOpts...)
}

func (s *searchTool) fallbackSearchDDG(ctx context.Context, req SearchRequest, log *slog.Logger, opts ...DDGOption) (string, error) {
	log.Info("Searching with DuckDuckGo (Fallback)", "query", req.Query)
	input := map[string]string{"query": req.Query}
	inputBytes, _ := json.Marshal(input)

	// Instantiate a fresh DDG tool for the fallback to ensure clean state
	// Pass any options (e.g. for testing)
	callable := NewDDGTool(opts...)
	res, err := callable.Call(ctx, inputBytes)
	if err != nil {
		return "", fmt.Errorf("fallback search failed: %w", err)
	}
	return fmt.Sprintf("[Source: DuckDuckGo]\n%v", res), nil
}

// searchBackend delegates to the underlying tool.CallableTool (Bing or DDG).
func (s *searchTool) searchBackend(ctx context.Context, req SearchRequest, log *slog.Logger, name string) (string, error) {
	if s.backend == nil {
		return "", fmt.Errorf("%s search not available: backend is nil", name)
	}
	log.Info("Searching with "+name, "query", req.Query)

	input := map[string]string{"query": req.Query}
	inputBytes, _ := json.Marshal(input)

	callable, ok := s.backend.(tool.CallableTool)
	if !ok {
		return "", fmt.Errorf("%s search tool is not callable", name)
	}
	res, err := callable.Call(ctx, inputBytes)
	if err != nil {
		return "", fmt.Errorf("%s search failed: %w", strings.ToLower(name), err)
	}
	return fmt.Sprintf("[Source: %s]\n%v", name, res), nil
}

// ────────────────────── Google ──────────────────────

func initGoogle(cfg Config) tool.Tool {
	if cfg.GoogleAPIKey == "" || cfg.GoogleCX == "" {
		return nil
	}
	gToolSet, err := search.NewToolSet(
		context.Background(),
		search.WithEngineID(cfg.GoogleCX),
		search.WithAPIKey(cfg.GoogleAPIKey),
	)
	if err != nil {
		return nil
	}
	tools := gToolSet.Tools(context.Background())
	if len(tools) > 0 {
		return tools[0]
	}
	return nil
}

func (s *searchTool) searchGoogle(ctx context.Context, req SearchRequest, log *slog.Logger) (string, error) {
	if s.backend == nil {
		return "", fmt.Errorf("google search not available: missing API key or CX")
	}
	log.Info("Searching with Google", "query", req.Query)

	googleInput := map[string]interface{}{
		"query": req.Query,
		"size":  5,
	}
	inputBytes, _ := json.Marshal(googleInput)

	if callable, ok := s.backend.(tool.CallableTool); ok {
		res, err := callable.Call(ctx, inputBytes)
		if err != nil {
			return "", fmt.Errorf("google search failed: %w", err)
		}
		resBytes, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("failed to marshal Google search result: %w", err)
		}
		return string(resBytes), nil
	}
	return "", fmt.Errorf("google search tool is not callable")
}

// normaliseProvider returns a canonical provider name, defaulting to duckduckgo.
func normaliseProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case ProviderGoogle:
		return ProviderGoogle
	case ProviderBing:
		return ProviderBing
	case ProviderDuckDuckGo, "ddg", "":
		return ProviderDuckDuckGo
	default:
		return ProviderDuckDuckGo
	}
}
