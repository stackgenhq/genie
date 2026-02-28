package websearch

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ddgUserAgents is a list of modern browser User-Agents to rotate through.
// DuckDuckGo blocks requests with non-standard User-Agents (HTTP 403).
var ddgUserAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/115.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/115.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/115.0",
}

const (
	// ddgHTMLEndpoint is the DuckDuckGo HTML search endpoint that returns real
	// web search results, unlike the Instant Answer API which only handles
	// factual/encyclopedic queries.
	ddgHTMLEndpoint = "https://html.duckduckgo.com/html/"

	// ddgTimeout is the HTTP timeout for DuckDuckGo search requests.
	// Kept short (12s) so that when DDG is down, the agent fails fast and
	// can pivot to http_request within ~25s instead of ~127s.
	ddgTimeout = 12 * time.Second

	// ddgMaxResults is the maximum number of search results to return.
	ddgMaxResults = 5

	// ddgMaxRetries is the maximum number of retry attempts for transient
	// failures. With ddgTimeout=12s this gives a worst-case of ~25s per
	// search (vs ~127s with the old 30s×4 config).
	ddgMaxRetries = 1
)

// ddgHTMLResult holds a single parsed search result from DuckDuckGo HTML search.
type ddgHTMLResult struct {
	title   string
	url     string
	snippet string
}

// ddgHTMLTool performs web searches by scraping the DuckDuckGo HTML search page.
// This approach returns real web search results (news, documentation, etc.) unlike
// the Instant Answer API which is limited to encyclopedic/factual queries.
// Without this tool, the agent cannot perform general-purpose web searches when
// Google or Bing API keys are unavailable.
type ddgHTMLTool struct {
	httpClient *retryablehttp.Client
	endpoint   string
}

// DDGOption is a functional option for configuring the DuckDuckGo HTML tool.
type DDGOption func(*ddgHTMLTool)

// WithDDGHTTPClient sets a custom HTTP client for the DuckDuckGo tool.
// Useful for testing with httptest.Server or for custom transport configuration.
// The provided *http.Client is wrapped in a retryablehttp.Client with DDG-specific
// retry settings.
func WithDDGHTTPClient(c *http.Client) DDGOption {
	return func(t *ddgHTMLTool) {
		t.httpClient.HTTPClient = c
	}
}

// WithDDGEndpoint overrides the default DuckDuckGo HTML endpoint.
// Primarily used for testing with httptest.Server.
func WithDDGEndpoint(endpoint string) DDGOption {
	return func(t *ddgHTMLTool) {
		t.endpoint = endpoint
	}
}

// ddgCheckRetry extends the default retry policy to also retry on HTTP 202.
// DuckDuckGo returns 202 (Accepted) as a rate-limit or challenge signal.
func ddgCheckRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err == nil && resp != nil && resp.StatusCode == http.StatusAccepted {
		return true, nil
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

// newDDGRetryClient creates a retryablehttp.Client configured for DuckDuckGo.
func newDDGRetryClient() *retryablehttp.Client {
	c := retryablehttp.NewClient()
	c.RetryMax = ddgMaxRetries
	c.RetryWaitMin = 1 * time.Second
	c.RetryWaitMax = 10 * time.Second
	c.CheckRetry = ddgCheckRetry
	c.HTTPClient.Timeout = ddgTimeout
	c.Logger = nil // suppress noisy debug logs
	return c
}

// NewDDGTool creates a new DuckDuckGo HTML search tool that scrapes real web
// search results from html.duckduckgo.com. This replaces the Instant Answer API
// tool which only worked for factual/encyclopedic queries.
// Without this tool, the agent has no free, key-less web search fallback and would
// fail on any query that isn't a well-known entity or definition.
func NewDDGTool(opts ...DDGOption) tool.CallableTool {
	t := &ddgHTMLTool{
		httpClient: newDDGRetryClient(),
		endpoint:   ddgHTMLEndpoint,
	}

	for _, opt := range opts {
		opt(t)
	}

	return function.NewFunctionTool(
		t.Search,
		function.WithName("duckduckgo_search"),
		function.WithDescription("Search the web using DuckDuckGo. Returns real web search "+
			"results including news, documentation, forums, and any other publicly indexed pages."),
	)
}

// DDGSearchRequest is the input schema for the DuckDuckGo HTML search tool.
type DDGSearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to execute"`
}

// Search performs a DuckDuckGo HTML search and returns formatted results.
// It POSTs to the DuckDuckGo HTML endpoint, parses the response HTML, and
// returns up to ddgMaxResults results as a numbered list.
//
// Transient failures (HTTP 202, 429, 5xx) are automatically retried via
// hashicorp/go-retryablehttp with exponential backoff.
func (t *ddgHTMLTool) Search(ctx context.Context, req DDGSearchRequest) (string, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return "", fmt.Errorf("empty search query provided")
	}

	form := url.Values{"q": {query}}
	httpReq, err := retryablehttp.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("ddg search: failed to build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Randomly select a User-Agent for this request to avoid blocking.
	// We use crypto/rand to avoid global lock contention in math/rand.
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(ddgUserAgents))))
	if err != nil {
		// Fallback to first UA if random fails (unlikely)
		n = big.NewInt(0)
	}
	ua := ddgUserAgents[n.Int64()]
	httpReq.Header.Set("User-Agent", ua)

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("duckduckgo search failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ddg search: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ddg search returned HTTP %d", resp.StatusCode)
	}

	results := parseDDGHTML(string(body))
	if len(results) == 0 {
		return fmt.Sprintf("No results found for '%s'.", query), nil
	}

	return formatDDGResults(results), nil
}

// reResultAnchor matches the result title anchor tag.
// Example: <a rel="nofollow" class="result__a" href="https://example.com">Title</a>
var reResultAnchor = regexp.MustCompile(`<a[^>]+class="result__a"[^>]+href="([^"]*)"[^>]*>(.*?)</a>`)

// reResultSnippet matches the result snippet anchor tag.
// Example: <a class="result__snippet" href="...">Snippet text with <b>bold</b> words</a>
var reResultSnippet = regexp.MustCompile(`<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)

// reHTMLTag matches any HTML tag for stripping.
var reHTMLTag = regexp.MustCompile(`<[^>]*>`)

// reDDGRedirect matches DuckDuckGo redirect URLs and extracts the target.
// Example: //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&rut=...
var reDDGRedirect = regexp.MustCompile(`//duckduckgo\.com/l/\?uddg=([^&]+)`)

// parseDDGHTML extracts search results from DuckDuckGo's HTML search page.
// The HTML structure uses consistent CSS classes: result__a for title links,
// result__snippet for description text. This function parses up to ddgMaxResults
// results from the HTML body.
func parseDDGHTML(html string) []ddgHTMLResult {
	titleMatches := reResultAnchor.FindAllStringSubmatch(html, ddgMaxResults)
	snippetMatches := reResultSnippet.FindAllStringSubmatch(html, ddgMaxResults)

	var results []ddgHTMLResult
	for i, m := range titleMatches {
		if len(m) < 3 {
			continue
		}

		rawURL := m[1]
		title := stripHTML(m[2])

		resolvedURL := resolveDDGURL(rawURL)

		var snippet string
		if i < len(snippetMatches) && len(snippetMatches[i]) >= 2 {
			snippet = stripHTML(snippetMatches[i][1])
		}

		results = append(results, ddgHTMLResult{
			title:   title,
			url:     resolvedURL,
			snippet: snippet,
		})
	}

	return results
}

// resolveDDGURL resolves a DuckDuckGo redirect URL to its target.
// DuckDuckGo wraps result URLs in redirects like //duckduckgo.com/l/?uddg=ENCODED_URL.
// This function extracts and decodes the target URL. If the URL is not a redirect,
// it is returned as-is.
func resolveDDGURL(rawURL string) string {
	if m := reDDGRedirect.FindStringSubmatch(rawURL); len(m) >= 2 {
		decoded, err := url.QueryUnescape(m[1])
		if err == nil {
			return decoded
		}
	}

	// Not a redirect — return as-is, adding https if protocol-relative.
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	return rawURL
}

// stripHTML removes all HTML tags and decodes common HTML entities.
func stripHTML(s string) string {
	s = reHTMLTag.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.TrimSpace(s)
	return s
}

// formatDDGResults formats parsed DuckDuckGo results as a numbered list.
func formatDDGResults(results []ddgHTMLResult) string {
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n", i+1, r.title, r.url, r.snippet)
	}
	return sb.String()
}
