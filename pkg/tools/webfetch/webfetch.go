// Package webfetch provides a URL content fetcher for agents. This is a
// critical fallback when web_search is unavailable — agents can directly
// fetch and read content from known URLs. The tool fetches a webpage via
// HTTP and returns a simplified text/markdown representation.
//
// Problem: Agents often need to read documentation, API responses, or web
// pages. Without this tool, they would have no way to access URL content
// directly — web_search only returns summaries, not full page content.
//
// Safety guards:
//   - 30-second HTTP timeout prevents hanging on slow servers
//   - Response body capped at 1 MB to prevent memory exhaustion
//   - HTML is cleaned via htmlutils (strips scripts, styles, nav, footer)
//   - Output truncated at 1 MB to limit LLM context consumption
//
// Dependencies:
//   - github.com/stackgenhq/genie/pkg/htmlutils — HTML-to-text extraction
//   - Go stdlib net/http — no external system dependencies
package webfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/htmlutils"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	defaultTimeout   = 30 * time.Second
	maxBodyBytes     = 1 << 20 // 1 MB
	defaultUserAgent = "Genie-WebFetch/1.0"
)

// ────────────────────── Request / Response ──────────────────────

type fetchRequest struct {
	URL     string            `json:"url" jsonschema:"description=The URL to fetch content from."`
	Method  string            `json:"method,omitempty" jsonschema:"description=HTTP method (GET, POST, HEAD). Defaults to GET.,enum=GET,enum=POST,enum=HEAD"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=Optional HTTP headers to send with the request."`
	Body    string            `json:"body,omitempty" jsonschema:"description=Optional request body for POST requests."`
}

type fetchResponse struct {
	URL         string `json:"url"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type fetchTools struct {
	client *http.Client
}

func newFetchTools() *fetchTools {
	return &fetchTools{
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

func (f *fetchTools) fetchTool() tool.CallableTool {
	return function.NewFunctionTool(
		f.fetch,
		function.WithName("web_fetch"),
		function.WithDescription(
			"Fetch content from a URL. Returns the page content as simplified text. "+
				"Use this to read web pages, API endpoints, documentation, or any HTTP resource. "+
				"Supports GET, POST, and HEAD methods. "+
				"For HTML pages, returns a text representation with the page title extracted.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (f *fetchTools) fetch(ctx context.Context, req fetchRequest) (fetchResponse, error) {
	resp := fetchResponse{URL: req.URL}

	if req.URL == "" {
		return resp, fmt.Errorf("url is required")
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bodyReader)
	if err != nil {
		return resp, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := f.client.Do(httpReq)
	if err != nil {
		return resp, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	resp.StatusCode = httpResp.StatusCode
	resp.ContentType = httpResp.Header.Get("Content-Type")

	if method == "HEAD" {
		resp.Message = fmt.Sprintf("HEAD %s → %d %s", req.URL, resp.StatusCode, http.StatusText(resp.StatusCode))
		return resp, nil
	}

	// Read body with size limit.
	limited := io.LimitReader(httpResp.Body, maxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return resp, fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(body)

	// If HTML, use the project's htmlutils for robust parsing-based text
	// extraction (handles malformed HTML, strips scripts/styles/nav/footer).
	if strings.Contains(resp.ContentType, "text/html") {
		resp.Title = f.extractTitle(content)
		content = htmlutils.ExtractText(content)
	}

	// Truncate if very large, reserving space for the truncation notice.
	truncNotice := "\n\n[Content truncated — exceeded 1 MB limit]"
	if len(content) > maxBodyBytes {
		content = content[:maxBodyBytes-len(truncNotice)] + truncNotice
	}

	resp.Content = strings.TrimSpace(content)
	resp.Message = fmt.Sprintf("Fetched %s → %d (%d bytes)", req.URL, resp.StatusCode, len(resp.Content))
	return resp, nil
}

// ────────────────────── HTML helpers ──────────────────────

// reTitle extracts the <title> element. We keep this here because
// htmlutils.ExtractText strips titles as part of <head>. We need the title
// separately for the response metadata.
var reTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func (f *fetchTools) extractTitle(html string) string {
	match := reTitle.FindStringSubmatch(html)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}
