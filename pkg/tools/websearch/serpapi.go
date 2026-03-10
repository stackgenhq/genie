// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

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
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// serpAPIEndpoint is the base URL for the SerpAPI JSON search endpoint.
// All engines (google, google_news, google_scholar) share this endpoint
// and are differentiated by the "engine" query parameter.
const serpAPIEndpoint = "https://serpapi.com/search.json"

// serpAPIMaxResults is the maximum number of results to return per search.
const serpAPIMaxResults = 10

// SerpAPIConfig holds configuration for the SerpAPI provider.
// SerpAPI is a paid search API that provides structured JSON results
// from Google Search, Google News, Google Scholar, and many other engines.
// See https://serpapi.com/search-api for the full API reference.
type SerpAPIConfig struct {
	APIKey   string `yaml:"serpapi_api_key,omitempty" toml:"serpapi_api_key,omitempty"`
	Location string `yaml:"location,omitempty" toml:"location,omitempty"` // e.g. "Austin, Texas, United States"
	GL       string `yaml:"gl,omitempty" toml:"gl,omitempty"`             // country code, e.g. "us"
	HL       string `yaml:"hl,omitempty" toml:"hl,omitempty"`             // language code, e.g. "en"
}

// serpAPITool wraps SerpAPI as a trpc-agent tool for a specific engine.
// Each engine (google, google_news, google_scholar) is instantiated as
// a separate tool with its own name and description.
type serpAPITool struct {
	apiKey   string
	engine   string // "google", "google_news", or "google_scholar"
	location string
	gl       string
	hl       string
	endpoint string // overridable for testing
}

// SerpAPIOption is a functional option for configuring SerpAPI tools.
type SerpAPIOption func(*serpAPITool)

// WithSerpAPIEndpoint overrides the default SerpAPI endpoint.
// Primarily used for testing with httptest.Server.
func WithSerpAPIEndpoint(endpoint string) SerpAPIOption {
	return func(t *serpAPITool) {
		t.endpoint = endpoint
	}
}

// SerpAPISearchRequest is the input schema for SerpAPI search tools.
type SerpAPISearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to execute"`
}

// NewSerpAPITool creates a new SerpAPI search tool for Google web search.
// This tool calls the SerpAPI Google Search endpoint and returns structured
// organic results including title, link, and snippet.
// Without this tool, users would need a Google Custom Search Engine (CSE) ID
// and API key, which are harder to set up than SerpAPI's single API key.
func NewSerpAPITool(cfg SerpAPIConfig, opts ...SerpAPIOption) tool.CallableTool {
	t := &serpAPITool{
		apiKey:   cfg.APIKey,
		engine:   "google",
		location: cfg.Location,
		gl:       cfg.GL,
		hl:       cfg.HL,
		endpoint: serpAPIEndpoint,
	}
	for _, opt := range opts {
		opt(t)
	}
	return function.NewFunctionTool(
		t.Search,
		function.WithName("serpapi_search"),
		function.WithDescription("Search the web using SerpAPI (Google Search). Returns structured organic search results with titles, links, and snippets. Supports location and language targeting."),
	)
}

// NewSerpAPINewsTool creates a SerpAPI Google News search tool.
// Uses engine=google_news to search for current news articles.
// The returned results include title, source, date, link, and snippet
// from Google News. This is useful when the agent needs recent news
// or current events rather than general web pages.
func NewSerpAPINewsTool(cfg SerpAPIConfig, opts ...SerpAPIOption) tool.CallableTool {
	t := &serpAPITool{
		apiKey:   cfg.APIKey,
		engine:   "google_news",
		location: cfg.Location,
		gl:       cfg.GL,
		hl:       cfg.HL,
		endpoint: serpAPIEndpoint,
	}
	for _, opt := range opts {
		opt(t)
	}
	return function.NewFunctionTool(
		t.Search,
		function.WithName("google_news_search"),
		function.WithDescription("Search Google News via SerpAPI for current news articles. Returns news results with title, source, date, link, and snippet. Use this when you need recent news or current events information."),
	)
}

// NewSerpAPIScholarTool creates a SerpAPI Google Scholar search tool.
// Uses engine=google_scholar to search for academic papers and publications.
// The returned results include title, authors, publication info, link,
// and snippet. This is useful when the agent needs academic or scientific
// information such as research papers, citations, or peer-reviewed content.
func NewSerpAPIScholarTool(cfg SerpAPIConfig, opts ...SerpAPIOption) tool.CallableTool {
	t := &serpAPITool{
		apiKey:   cfg.APIKey,
		engine:   "google_scholar",
		location: cfg.Location,
		gl:       cfg.GL,
		hl:       cfg.HL,
		endpoint: serpAPIEndpoint,
	}
	for _, opt := range opts {
		opt(t)
	}
	return function.NewFunctionTool(
		t.Search,
		function.WithName("google_scholar_search"),
		function.WithDescription("Search Google Scholar via SerpAPI for academic papers and publications. Returns scholarly results with title, authors, publication info, link, and snippet. Use this for research papers, citations, or scientific information."),
	)
}

// Search executes a SerpAPI search query using the configured engine.
// It constructs the API request with the query, API key, and optional
// location/language parameters, then formats the response based on the engine type.
func (t *serpAPITool) Search(ctx context.Context, req SerpAPISearchRequest) (string, error) {
	if t.apiKey == "" {
		return "", fmt.Errorf("serpapi search not available: missing serpapi_api_key")
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return "", fmt.Errorf("empty search query provided")
	}

	log := logger.GetLogger(ctx)
	log.Info("Searching with SerpAPI", "engine", t.engine, "query", query)

	u, err := url.Parse(t.endpoint)
	if err != nil {
		return "", fmt.Errorf("serpapi: invalid endpoint: %w", err)
	}
	q := u.Query()
	q.Set("engine", t.engine)
	q.Set("q", query)
	q.Set("api_key", t.apiKey)
	q.Set("num", fmt.Sprintf("%d", serpAPIMaxResults))

	if t.location != "" {
		q.Set("location", t.location)
	}
	if t.gl != "" {
		q.Set("gl", t.gl)
	}
	if t.hl != "" {
		q.Set("hl", t.hl)
	}

	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("serpapi: failed to build request: %w", err)
	}

	client := httputil.GetClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("serpapi search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("serpapi: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("serpapi returned HTTP %d: %s", resp.StatusCode, truncateBody(body, 200))
	}

	switch t.engine {
	case "google_news":
		return formatSerpAPINewsResults(body)
	case "google_scholar":
		return formatSerpAPIScholarResults(body)
	default:
		return formatSerpAPIOrganicResults(body)
	}
}

// ────────────────────── Response formatting ──────────────────────

// serpAPIOrganicResponse is the relevant subset of a SerpAPI Google Search response.
type serpAPIOrganicResponse struct {
	OrganicResults []struct {
		Position int    `json:"position"`
		Title    string `json:"title"`
		Link     string `json:"link"`
		Snippet  string `json:"snippet"`
		Date     string `json:"date,omitempty"`
	} `json:"organic_results"`
	AnswerBox *struct {
		Title   string `json:"title"`
		Answer  string `json:"answer"`
		Snippet string `json:"snippet"`
		Link    string `json:"link"`
	} `json:"answer_box,omitempty"`
	KnowledgeGraph *struct {
		Title       string `json:"title"`
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"knowledge_graph,omitempty"`
}

// formatSerpAPIOrganicResults parses and formats a SerpAPI Google Search JSON response
// into a human-readable numbered list. Includes answer box and knowledge graph when present.
func formatSerpAPIOrganicResults(body []byte) (string, error) {
	var resp serpAPIOrganicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("serpapi: failed to parse response: %w", err)
	}

	var sb strings.Builder

	// Include answer box if present (direct answers like definitions, calculations)
	if resp.AnswerBox != nil {
		sb.WriteString("[Answer Box]\n")
		if resp.AnswerBox.Answer != "" {
			sb.WriteString(resp.AnswerBox.Answer)
			sb.WriteString("\n")
		}
		if resp.AnswerBox.Snippet != "" {
			sb.WriteString(resp.AnswerBox.Snippet)
			sb.WriteString("\n")
		}
		if resp.AnswerBox.Link != "" {
			sb.WriteString(resp.AnswerBox.Link)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Include knowledge graph if present
	if resp.KnowledgeGraph != nil && resp.KnowledgeGraph.Description != "" {
		sb.WriteString("[Knowledge Graph: ")
		sb.WriteString(resp.KnowledgeGraph.Title)
		if resp.KnowledgeGraph.Type != "" {
			sb.WriteString(" (")
			sb.WriteString(resp.KnowledgeGraph.Type)
			sb.WriteString(")")
		}
		sb.WriteString("]\n")
		sb.WriteString(resp.KnowledgeGraph.Description)
		sb.WriteString("\n\n")
	}

	if len(resp.OrganicResults) == 0 && resp.AnswerBox == nil {
		return "No results found.", nil
	}

	sb.WriteString("[Source: SerpAPI / Google]\n")
	for i, r := range resp.OrganicResults {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.Link, r.Snippet)
	}

	return sb.String(), nil
}

// serpAPINewsResponse is the relevant subset of a SerpAPI Google News response.
type serpAPINewsResponse struct {
	NewsResults []struct {
		Position int    `json:"position"`
		Title    string `json:"title"`
		Link     string `json:"link"`
		Source   string `json:"source"`
		Date     string `json:"date"`
		Snippet  string `json:"snippet"`
	} `json:"news_results"`
}

// formatSerpAPINewsResults parses and formats a SerpAPI Google News JSON response
// into a human-readable numbered list including source and date.
func formatSerpAPINewsResults(body []byte) (string, error) {
	var resp serpAPINewsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("serpapi: failed to parse news response: %w", err)
	}

	if len(resp.NewsResults) == 0 {
		return "No news results found.", nil
	}

	var sb strings.Builder
	sb.WriteString("[Source: SerpAPI / Google News]\n")
	for i, r := range resp.NewsResults {
		fmt.Fprintf(&sb, "%d. %s\n   Source: %s | Date: %s\n   %s\n   %s\n",
			i+1, r.Title, r.Source, r.Date, r.Link, r.Snippet)
	}

	return sb.String(), nil
}

// serpAPIScholarResponse is the relevant subset of a SerpAPI Google Scholar response.
type serpAPIScholarResponse struct {
	OrganicResults []struct {
		Position        int    `json:"position"`
		Title           string `json:"title"`
		Link            string `json:"link"`
		Snippet         string `json:"snippet"`
		PublicationInfo struct {
			Summary string `json:"summary"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
		} `json:"publication_info"`
		InlineLinks struct {
			CitedBy struct {
				Total int    `json:"total"`
				Link  string `json:"link"`
			} `json:"cited_by"`
		} `json:"inline_links"`
	} `json:"organic_results"`
}

// formatSerpAPIScholarResults parses and formats a SerpAPI Google Scholar JSON response
// into a human-readable numbered list including authors, publication info, and citation count.
func formatSerpAPIScholarResults(body []byte) (string, error) {
	var resp serpAPIScholarResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("serpapi: failed to parse scholar response: %w", err)
	}

	if len(resp.OrganicResults) == 0 {
		return "No scholar results found.", nil
	}

	var sb strings.Builder
	sb.WriteString("[Source: SerpAPI / Google Scholar]\n")
	for i, r := range resp.OrganicResults {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, r.Title)

		// Authors
		if len(r.PublicationInfo.Authors) > 0 {
			authors := make([]string, 0, len(r.PublicationInfo.Authors))
			for _, a := range r.PublicationInfo.Authors {
				authors = append(authors, a.Name)
			}
			fmt.Fprintf(&sb, "   Authors: %s\n", strings.Join(authors, ", "))
		}

		// Publication info
		if r.PublicationInfo.Summary != "" {
			fmt.Fprintf(&sb, "   Published: %s\n", r.PublicationInfo.Summary)
		}

		if r.Link != "" {
			fmt.Fprintf(&sb, "   %s\n", r.Link)
		}

		if r.InlineLinks.CitedBy.Total > 0 {
			fmt.Fprintf(&sb, "   Cited by: %d\n", r.InlineLinks.CitedBy.Total)
		}

		if r.Snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", r.Snippet)
		}
	}

	return sb.String(), nil
}

// truncateBody truncates a byte slice to maxLen for error messages.
// Avoids overwhelming error logs when the API returns large error bodies.
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
