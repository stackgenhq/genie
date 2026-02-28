package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/stackgenhq/genie/pkg/httputil"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const bingEndpoint = "https://api.bing.microsoft.com/v7.0/search"

// BingSearchRequest is the input schema for the standalone Bing search tool.
type BingSearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to execute"`
}

// bingResponse is a minimal representation of the Bing Web Search API v7 response.
type bingResponse struct {
	WebPages struct {
		Value []struct {
			Name    string `json:"name"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"value"`
	} `json:"webPages"`
}

// bingTool wraps Bing Web Search API v7 as a trpc-agent tool.
type bingTool struct {
	apiKey string
}

// NewBingTool creates a new Bing Web Search tool wrapped in the trpc-agent
// tool schema. The returned tool has the name "bing_search" and can be tested
// or composed independently.
func NewBingTool(apiKey string) tool.CallableTool {
	bt := &bingTool{apiKey: apiKey}
	return function.NewFunctionTool(
		bt.Search,
		function.WithName("bing_search"),
		function.WithDescription("Search the web using Bing Web Search API v7."),
	)
}

// Search executes a Bing Web Search API v7 query.
func (bt *bingTool) Search(ctx context.Context, req BingSearchRequest) (string, error) {
	if bt.apiKey == "" {
		return "", fmt.Errorf("bing search not available: missing bing_api_key")
	}

	u, _ := url.Parse(bingEndpoint)
	q := u.Query()
	q.Set("q", req.Query)
	q.Set("count", "5")
	q.Set("textFormat", "Raw")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("bing search: failed to build request: %w", err)
	}
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", bt.apiKey)

	client := httputil.GetClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("bing search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("bing search: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bing search returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var bingResp bingResponse
	if err := json.Unmarshal(body, &bingResp); err != nil {
		return "", fmt.Errorf("bing search: failed to parse response: %w", err)
	}

	return formatBingResults(bingResp), nil
}

func formatBingResults(resp bingResponse) string {
	if len(resp.WebPages.Value) == 0 {
		return "No results found."
	}
	var sb strings.Builder
	for i, r := range resp.WebPages.Value {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n", i+1, r.Name, r.URL, r.Snippet)
	}
	return sb.String()
}
