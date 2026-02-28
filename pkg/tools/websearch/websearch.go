package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
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
// When Provider is "google", either set GoogleAPIKey+GoogleCX (API key auth; 100 free queries/day, then billing) or set
// UseGoogleOAuth true and GoogleCX (uses shared Google sign-in token; requires scope https://www.googleapis.com/auth/cse per Custom Search JSON API). Re-run the Google OAuth browser flow if the token was created before the cse scope was added.
type Config struct {
	Provider       string `yaml:"provider,omitempty" toml:"provider,omitempty"`
	GoogleAPIKey   string `yaml:"google_api_key,omitempty" toml:"google_api_key,omitempty"`
	GoogleCX       string `yaml:"google_cx,omitempty" toml:"google_cx,omitempty"`
	UseGoogleOAuth bool   `yaml:"use_google_oauth,omitempty" toml:"use_google_oauth,omitempty"` // use shared Google OAuth token for search when SecretProvider is set
	BingAPIKey     string `yaml:"bing_api_key,omitempty" toml:"bing_api_key,omitempty"`
}

// searchTool implements the web_search tool with a configurable backend.
type searchTool struct {
	cfg            Config
	backend        tool.Tool // the active provider's tool (DDG, Google, or Bing)
	provider       string    // normalised provider name
	ddgOpts        []DDGOption
	secretProvider security.SecretProvider // optional; when set, Google provider can use OAuth
}

// NewTool creates a new web search tool configured to use a single provider.
// When Provider is empty it defaults to DuckDuckGo which requires no API keys.
// If the selected provider is missing credentials, it logs a warning and falls back to DuckDuckGo.
// When provider is Google and secretProvider is non-nil, OAuth is used for search when
// UseGoogleOAuth is true or GoogleAPIKey is empty (same token as Calendar/Drive/Gmail).
//
// Optional DDGOptions can be provided to configure the fallback DuckDuckGo tool (e.g. for testing).
func NewTool(cfg Config, secretProvider security.SecretProvider, opts ...DDGOption) tool.CallableTool {
	s := &searchTool{
		cfg:            cfg,
		provider:       normaliseProvider(cfg.Provider),
		ddgOpts:        opts,
		secretProvider: secretProvider,
	}

	// Try to initialise the selected provider; fall back to DuckDuckGo
	// when credentials are missing or initialisation fails.
	switch s.provider {
	case ProviderGoogle:
		if cfg.GoogleAPIKey != "" && cfg.GoogleCX != "" {
			s.backend = initGoogle(cfg)
		}
		// If no API-key backend but OAuth is requested and we have SecretProvider + CX, use OAuth at request time.
	case ProviderBing:
		if cfg.BingAPIKey != "" {
			s.backend = NewBingTool(cfg.BingAPIKey)
		}
	}
	// Google with OAuth only (no API key): backend stays nil; searchGoogle will use OAuth at request time.
	// Fall back to DuckDuckGo only when Google is selected but credentials are missing for the chosen mode.
	if s.backend == nil && (s.provider != ProviderGoogle || (s.cfg.UseGoogleOAuth && s.secretProvider == nil) || s.cfg.GoogleCX == "" || (!s.cfg.UseGoogleOAuth && s.cfg.GoogleAPIKey == "")) {
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
		result, err = s.searchGoogle(ctx, req)
	case ProviderBing:
		result, err = s.searchBackend(ctx, req, "Bing")
	default:
		// DuckDuckGo is the primary (and only) provider — no fallback.
		result, err = s.searchBackend(ctx, req, "DuckDuckGo")
		if err != nil {
			return "", fmt.Errorf(
				"web_search failed for query %q: %w. "+
					"Search is unavailable — use http_request to visit relevant websites directly instead",
				req.Query, err,
			)
		}
		return result, nil
	}

	// Google/Bing succeeded?
	if err == nil {
		return result, nil
	}

	// Google/Bing failed — fall back to DDG
	log.Warn("web_search: primary provider failed, falling back to duckduckgo",
		"provider", s.provider,
		"error", err,
	)

	result, fbErr := s.fallbackSearchDDG(ctx, req, s.ddgOpts...)
	if fbErr != nil {
		return "", fmt.Errorf(
			"web_search failed for query %q (primary: %s, fallback: DuckDuckGo). "+
				"Search services may be rate-limited or unavailable. "+
				"Try using http_request to visit relevant websites directly instead of searching",
			req.Query, s.provider,
		)
	}
	return result, nil
}

func (s *searchTool) fallbackSearchDDG(ctx context.Context, req SearchRequest, opts ...DDGOption) (string, error) {
	logger.GetLogger(ctx).Info("Searching with DuckDuckGo (Fallback)", "query", req.Query)
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
func (s *searchTool) searchBackend(ctx context.Context, req SearchRequest, name string) (string, error) {
	if s.backend == nil {
		return "", fmt.Errorf("%s search not available: backend is nil", name)
	}
	logger.GetLogger(ctx).Info("Searching with "+name, "query", req.Query)

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

func (s *searchTool) searchGoogle(ctx context.Context, req SearchRequest) (string, error) {
	log := logger.GetLogger(ctx)
	// Prefer OAuth when configured: same Google sign-in as Calendar/Drive/Gmail.
	if s.secretProvider != nil && s.cfg.GoogleCX != "" && (s.cfg.GoogleAPIKey == "" || s.cfg.UseGoogleOAuth) {
		result, err := s.searchGoogleWithOAuth(ctx, req.Query)
		if err == nil {
			return result, nil
		}
		log.Warn("Google search with OAuth failed, trying API key or falling back", "error", err)
	}
	if s.backend == nil {
		return "", fmt.Errorf("google search not available: set google_api_key and google_cx for API-key auth, or use_google_oauth with google_cx and a Google OAuth token (e.g. same integration as Calendar/Drive)")
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

// searchGoogleWithOAuth calls the Custom Search JSON API using the shared Google OAuth token.
// Same token as Calendar, Drive, Gmail; one sign-in can power search too.
func (s *searchTool) searchGoogleWithOAuth(ctx context.Context, query string) (string, error) {
	credsEntry, _ := s.secretProvider.GetSecret(ctx, "CredentialsFile")
	credsJSON, err := oauth.GetCredentials(credsEntry, "Custom Search")
	if err != nil {
		return "", fmt.Errorf("google search oauth credentials: %w", err)
	}
	tokenJSON, save, err := oauth.GetToken(ctx, s.secretProvider)
	if err != nil {
		return "", fmt.Errorf("google search oauth token: %w", err)
	}
	// Custom Search JSON API requires OAuth scope https://www.googleapis.com/auth/cse (see cse.list reference).
	scopes := []string{"https://www.googleapis.com/auth/cse"}
	client, err := oauth.HTTPClient(ctx, credsJSON, tokenJSON, save, scopes)
	if err != nil {
		return "", fmt.Errorf("google search oauth client: %w", err)
	}
	apiURL, err := url.Parse("https://www.googleapis.com/customsearch/v1")
	if err != nil {
		return "", fmt.Errorf("custom search base URL: %w", err)
	}
	q := apiURL.Query()
	q.Set("cx", s.cfg.GoogleCX)
	q.Set("q", query)
	q.Set("num", "5")
	apiURL.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("custom search request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("custom search API returned %d", resp.StatusCode)
	}
	var out struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("custom search response: %w", err)
	}
	var b strings.Builder
	b.WriteString("[Source: Google]\n")
	for i, it := range out.Items {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(it.Title)
		b.WriteString("\n")
		b.WriteString(it.Link)
		b.WriteString("\n")
		b.WriteString(it.Snippet)
	}
	return b.String(), nil
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
