// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package networking provides agent-callable tools for making HTTP requests
// (GET, POST, etc.) to arbitrary URLs with configurable headers, body, and
// timeout. It enables the agent to fetch APIs and web content when web_search
// or webfetch are insufficient or when a specific endpoint is needed.
package networking

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// HTTPRequest is the input schema for the http_request tool.
type HTTPRequest struct {
	URL     string            `json:"url"     jsonschema:"description=The URL to send the request to,required"`
	Method  string            `json:"method"  jsonschema:"description=HTTP method: GET POST PUT PATCH DELETE HEAD OPTIONS. Defaults to GET if omitted"`
	Headers map[string]string `json:"headers" jsonschema:"description=Optional HTTP headers as key-value pairs"`
	Body    string            `json:"body"    jsonschema:"description=Optional request body (for POST/PUT/PATCH). Sent as-is — use JSON strings for JSON APIs"`
	Timeout int               `json:"timeout" jsonschema:"description=Request timeout in seconds. Defaults to 30. Max 120"`
}

// Config holds optional configuration for the HTTP tool.
type Config struct {
	// MaxResponseBytes caps the response body read size. Default 512KB.
	MaxResponseBytes int64 `yaml:"max_response_bytes" toml:"max_response_bytes"`
	// DefaultTimeout in seconds when not specified per-request. Default 30.
	DefaultTimeout int `yaml:"default_timeout" toml:"default_timeout"`
}

const (
	defaultMaxResponseBytes int64 = 512 * 1024 // 512 KB
	defaultTimeout                = 30         // seconds
	maxTimeout                    = 120        // seconds

	// defaultUserAgent is sent when the caller doesn't set one.
	// Many sites (Wikipedia, Cloudflare-protected sites) reject requests
	// without a recognisable User-Agent with 403.
	defaultUserAgent = "Mozilla/5.0 (compatible; Genie/1.0; +https://stackgen.com)"
)

type httpTool struct {
	client           *http.Client
	maxResponseBytes int64
	defaultTimeout   int
}

// NewTool creates a new http_request tool.
func NewTool(cfg ...Config) tool.CallableTool {
	t := &httpTool{
		client:           &http.Client{},
		maxResponseBytes: defaultMaxResponseBytes,
		defaultTimeout:   defaultTimeout,
	}
	if len(cfg) > 0 {
		c := cfg[0]
		if c.MaxResponseBytes > 0 {
			t.maxResponseBytes = c.MaxResponseBytes
		}
		if c.DefaultTimeout > 0 {
			t.defaultTimeout = c.DefaultTimeout
		}
	}

	return function.NewFunctionTool(
		t.Do,
		function.WithName("http_request"),
		function.WithDescription(
			"Make an HTTP request to any URL. Supports GET, POST, PUT, PATCH, DELETE, HEAD, and OPTIONS. "+
				"Use this tool to call REST APIs, check endpoint health, fetch remote data, "+
				"or interact with webhooks. Response bodies are capped at 512KB.",
		),
	)
}

// Do executes the HTTP request and returns the response as a formatted string.
func (t *httpTool) Do(ctx context.Context, req HTTPRequest) (string, error) {
	log := logger.GetLogger(ctx).With("fn", "networking.http_request")

	// Defaults
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	// Validate method
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
		// ok
	default:
		return "", fmt.Errorf("unsupported HTTP method: %s", method)
	}

	if req.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	// Timeout
	timeout := t.defaultTimeout
	if req.Timeout > 0 {
		timeout = req.Timeout
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Build request
	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Auto-set Content-Type for body requests if not explicitly set
	if req.Body != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Auto-set User-Agent if the caller didn't provide one.
	// Many sites (Wikipedia, Cloudflare) reject bare requests with 403.
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", defaultUserAgent)
	}

	log.Info("executing HTTP request", "method", method, "url", req.URL)

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read body (capped). Read one extra byte to detect truncation.
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, t.maxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	truncated := int64(len(rawBody)) > t.maxResponseBytes
	body := rawBody
	if truncated {
		body = rawBody[:t.maxResponseBytes]
	}

	// Format response
	var sb strings.Builder
	fmt.Fprintf(&sb, "HTTP %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))

	// Include key response headers
	for _, h := range []string{"Content-Type", "Location", "X-Request-Id", "Retry-After"} {
		if v := resp.Header.Get(h); v != "" {
			fmt.Fprintf(&sb, "%s: %s\n", h, v)
		}
	}
	sb.WriteString("\n")

	// For HTTP error responses (4xx/5xx), return a short summary instead
	// of the full HTML body. Error pages (especially 404s) contain the
	// same nav/footer/country-picker boilerplate as normal pages (~500KB)
	// but carry no useful information. Sending them to the summarizer
	// wastes tokens.
	if resp.StatusCode >= 400 {
		snippet := string(body)
		const maxSnippet = 500
		if len(snippet) > maxSnippet {
			snippet = snippet[:maxSnippet] + "..."
		}
		fmt.Fprintf(&sb, "[Error page — body truncated to save tokens]\n%s", snippet)
		log.Info("HTTP error response, returning short summary",
			"method", method, "url", req.URL,
			"status", resp.StatusCode, "full_body_bytes", len(body))
		return sb.String(), nil
	}

	// Detect Cloudflare challenge pages. These return HTTP 200 but contain
	// a "Just a moment..." interstitial with no useful content. Without
	// detection, 265KB of challenge HTML flows to the summarizer and
	// produces a useless 90-char summary. The agent then retries the same
	// URL repeatedly. Returning a clear signal prevents this.
	if isCloudflareChallenge(body) {
		log.Info("Cloudflare challenge detected, returning signal",
			"method", method, "url", req.URL)
		fmt.Fprintf(&sb, "[Cloudflare challenge page — this site requires browser JavaScript to access. "+
			"Do NOT retry this URL. Try a different source or use a search engine to find cached/mirrored content.]")
		return sb.String(), nil
	}

	if len(body) > 0 {
		sb.Write(body)
		if truncated {
			fmt.Fprintf(&sb, "\n\n[Response truncated at %dKB]", t.maxResponseBytes/1024)
		}
	} else {
		sb.WriteString("[Empty response body]")
	}

	log.Info("HTTP request completed", "method", method, "url", req.URL,
		"status", resp.StatusCode, "body_bytes", len(body))

	return sb.String(), nil
}

// isCloudflareChallenge returns true if the response body looks like a
// Cloudflare "Just a moment..." challenge page or similar JS-required
// interstitial. These pages return HTTP 200 but contain no real content.
func isCloudflareChallenge(body []byte) bool {
	if len(body) < 100 {
		return false
	}
	// Only check the first 2KB for speed.
	check := body
	if len(check) > 2048 {
		check = check[:2048]
	}
	lower := strings.ToLower(string(check))
	// Cloudflare challenge markers
	if strings.Contains(lower, "just a moment") && strings.Contains(lower, "cf-browser-verification") {
		return true
	}
	if strings.Contains(lower, "_cf_chl_opt") {
		return true
	}
	// Generic JS-required interstitials
	if strings.Contains(lower, "enable javascript and cookies to continue") {
		return true
	}
	return false
}
