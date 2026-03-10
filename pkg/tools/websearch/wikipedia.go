// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package websearch

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

var wikipediaUserAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/115.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/115.0",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/115.0",
}

const (
	wikipediaMaxResults      = 5
	wikipediaTimeout         = 12 * time.Second
	wikipediaMaxRetries      = 1
	wikipediaDefaultEndpoint = "https://en.wikipedia.org/w/api.php"
)

type wikipediaResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type wikipediaTool struct {
	httpClient *retryablehttp.Client
	endpoint   string
}

type WikipediaOption func(*wikipediaTool)

func WithWikipediaEndpoint(endpoint string) WikipediaOption {
	return func(t *wikipediaTool) {
		t.endpoint = endpoint
	}
}

func WithWikipediaHTTPClient(c *http.Client) WikipediaOption {
	return func(t *wikipediaTool) {
		t.httpClient.HTTPClient = c
	}
}

func newWikipediaRetryClient() *retryablehttp.Client {
	c := retryablehttp.NewClient()
	c.RetryMax = wikipediaMaxRetries
	c.RetryWaitMin = 1 * time.Second
	c.RetryWaitMax = 10 * time.Second
	c.HTTPClient.Timeout = wikipediaTimeout
	c.Logger = nil
	return c
}

func NewWikipediaTool(opts ...WikipediaOption) tool.CallableTool {
	t := &wikipediaTool{
		httpClient: newWikipediaRetryClient(),
		endpoint:   wikipediaDefaultEndpoint,
	}

	for _, opt := range opts {
		opt(t)
	}

	return function.NewFunctionTool(
		t.Search,
		function.WithName("wikipedia_search"),
		function.WithDescription("Search Wikipedia for factual and encyclopedic information. Returns Wikipedia article titles, URLs, and snippets."),
	)
}

type WikipediaSearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to execute"`
}

func (t *wikipediaTool) Search(ctx context.Context, req WikipediaSearchRequest) (string, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return "", fmt.Errorf("empty search query provided")
	}

	u := t.endpoint + "?action=query&list=search&utf8=&format=json&srsearch=" + url.QueryEscape(query)
	httpReq, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("wikipedia search: failed to build request: %w", err)
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(wikipediaUserAgents))))
	if err != nil {
		n = big.NewInt(0)
	}
	ua := wikipediaUserAgents[n.Int64()]
	httpReq.Header.Set("User-Agent", ua)

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("wikipedia search failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("wikipedia search: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wikipedia search returned HTTP %d", resp.StatusCode)
	}

	var wikiResp struct {
		Query struct {
			Search []struct {
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
				PageID  int    `json:"pageid"`
			} `json:"search"`
		} `json:"query"`
	}

	if err := json.Unmarshal(body, &wikiResp); err != nil {
		return "", fmt.Errorf("wikipedia search: failed to parse response: %w", err)
	}

	var results []wikipediaResult
	for _, raw := range wikiResp.Query.Search {
		results = append(results, wikipediaResult{
			Title:   raw.Title,
			URL:     fmt.Sprintf("https://en.wikipedia.org/?curid=%d", raw.PageID),
			Snippet: stripHTMLWiki(raw.Snippet),
		})
		if len(results) >= wikipediaMaxResults {
			break
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("No results found for '%s'.", query), nil
	}

	return formatWikipediaResults(results), nil
}

func stripHTMLWiki(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&#39;", "'")

	for {
		start := strings.Index(s, "<")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], ">")
		if end == -1 {
			break
		}
		s = s[:start] + s[start+end+1:]
	}

	return strings.TrimSpace(s)
}

func formatWikipediaResults(results []wikipediaResult) string {
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Snippet)
	}
	return sb.String()
}
