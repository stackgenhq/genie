// Package pkgsearch provides package registry search tools for agents.
// It queries public package registries (Go modules, npm, PyPI) to find
// library versions, descriptions, and metadata — helping agents make
// informed decisions about dependencies.
//
// Problem: Agents need to recommend libraries, check version compatibility,
// and verify package existence. Without this tool, they would rely on
// potentially outdated training data for package information.
//
// Supported registries:
//   - npm (registry.npmjs.org) — JavaScript/TypeScript packages
//   - PyPI (pypi.org) — Python packages
//   - Go modules (pkg.go.dev + proxy.golang.org) — Go packages
//
// Testability: Base URLs for each registry are injectable via struct fields,
// allowing tests to use httptest.NewServer for deterministic, air-gap-safe
// testing with no real API calls.
//
// Dependencies:
//   - Go stdlib only (net/http, encoding/json)
//   - No external system dependencies
//   - No authentication required (all public APIs)
package pkgsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	defaultTimeout = 15 * time.Second
	userAgent      = "Genie-PkgSearch/1.0"
	maxResults     = 10
)

// ────────────────────── Request / Response ──────────────────────

type searchRequest struct {
	Registry string `json:"registry" jsonschema:"description=The package registry to search. One of: go, npm, pypi.,enum=go,enum=npm,enum=pypi"`
	Query    string `json:"query" jsonschema:"description=Search query or exact package name."`
}

type packageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

type searchResponse struct {
	Registry string        `json:"registry"`
	Query    string        `json:"query"`
	Results  []packageInfo `json:"results"`
	Count    int           `json:"count"`
	Message  string        `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type pkgTools struct {
	client      *http.Client
	npmBaseURL  string // injectable for testing; default: https://registry.npmjs.org
	pypiBaseURL string // injectable for testing; default: https://pypi.org
	goBaseURL   string // injectable for testing; default: https://pkg.go.dev
	goProxyURL  string // injectable for testing; default: https://proxy.golang.org
}

func newPkgTools() *pkgTools {
	return &pkgTools{
		client:      &http.Client{Timeout: defaultTimeout},
		npmBaseURL:  "https://registry.npmjs.org",
		pypiBaseURL: "https://pypi.org",
		goBaseURL:   "https://pkg.go.dev",
		goProxyURL:  "https://proxy.golang.org",
	}
}

func (p *pkgTools) searchTool() tool.CallableTool {
	return function.NewFunctionTool(
		p.search,
		function.WithName("util_pkg_search"),
		function.WithDescription(
			"Search package registries for libraries and their versions. "+
				"Supported registries: go (pkg.go.dev), npm (npmjs.com), pypi (pypi.org). "+
				"Returns package name, latest version, description, and URL. "+
				"Use this to find dependencies, check versions, or discover libraries.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (p *pkgTools) search(ctx context.Context, req searchRequest) (searchResponse, error) {
	registry := strings.ToLower(strings.TrimSpace(req.Registry))
	resp := searchResponse{
		Registry: registry,
		Query:    req.Query,
	}

	if req.Query == "" {
		return resp, fmt.Errorf("query is required")
	}

	switch registry {
	case "go":
		return p.searchGo(ctx, req.Query, resp)
	case "npm":
		return p.searchNPM(ctx, req.Query, resp)
	case "pypi":
		return p.searchPyPI(ctx, req.Query, resp)
	default:
		return resp, fmt.Errorf("unsupported registry %q: must be one of go, npm, pypi", req.Registry)
	}
}

// ────────────────────── Go (pkg.go.dev) ──────────────────────

func (p *pkgTools) searchGo(ctx context.Context, query string, resp searchResponse) (searchResponse, error) {
	// Use the pkg.go.dev search API.
	apiURL := fmt.Sprintf("%s/search?q=%s&m=package&limit=%d",
		p.goBaseURL, url.QueryEscape(query), maxResults)

	body, err := p.doGet(ctx, apiURL)
	if err != nil {
		return resp, fmt.Errorf("Go package search failed: %w", err)
	}

	// pkg.go.dev doesn't have a JSON API, so we try the proxy API for exact matches first.
	// For exact module lookup, try proxy.golang.org.
	proxyURL := fmt.Sprintf("%s/%s/@latest", p.goProxyURL, query)
	proxyBody, proxyErr := p.doGet(ctx, proxyURL)

	if proxyErr == nil {
		var modInfo struct {
			Version string `json:"Version"`
			Time    string `json:"Time"`
		}
		if json.Unmarshal(proxyBody, &modInfo) == nil && modInfo.Version != "" {
			resp.Results = append(resp.Results, packageInfo{
				Name:    query,
				Version: modInfo.Version,
				URL:     fmt.Sprintf("https://pkg.go.dev/%s", query),
			})
		}
	}

	// If we couldn't get results from proxy, provide a helpful message with search URL.
	if len(resp.Results) == 0 {
		// Parse the HTML search results minimally.
		results := p.parseGoSearchHTML(string(body), query)
		resp.Results = results
	}

	resp.Count = len(resp.Results)
	if resp.Count == 0 {
		resp.Message = fmt.Sprintf("No Go packages found for %q. Try browsing: https://pkg.go.dev/search?q=%s",
			query, url.QueryEscape(query))
	} else {
		resp.Message = fmt.Sprintf("Found %d Go package(s) for %q", resp.Count, query)
	}
	return resp, nil
}

// parseGoSearchHTML does minimal extraction from pkg.go.dev HTML search results.
func (p *pkgTools) parseGoSearchHTML(html, query string) []packageInfo {
	// This is a best-effort parser for the search results page.
	// We look for data-test-id="unit-name" patterns.
	var results []packageInfo
	lines := strings.Split(html, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for links that appear to be package paths.
		if strings.Contains(line, "/"+query) || strings.Contains(line, query+"/") {
			// Extract href from anchor tags.
			if idx := strings.Index(line, "href=\"/"); idx >= 0 {
				end := strings.Index(line[idx+6:], "\"")
				if end > 0 {
					path := line[idx+6 : idx+6+end]
					if strings.Contains(path, ".") && !strings.HasPrefix(path, "search") {
						results = append(results, packageInfo{
							Name: path,
							URL:  "https://pkg.go.dev/" + path,
						})
					}
				}
			}
		}
		if len(results) >= maxResults {
			break
		}
	}
	return results
}

// ────────────────────── npm (npmjs.com) ──────────────────────

func (p *pkgTools) searchNPM(ctx context.Context, query string, resp searchResponse) (searchResponse, error) {
	apiURL := fmt.Sprintf("%s/-/v1/search?text=%s&size=%d",
		p.npmBaseURL, url.QueryEscape(query), maxResults)

	body, err := p.doGet(ctx, apiURL)
	if err != nil {
		return resp, fmt.Errorf("npm search failed: %w", err)
	}

	var npmResp struct {
		Objects []struct {
			Package struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Description string `json:"description"`
				Links       struct {
					NPM string `json:"npm"`
				} `json:"links"`
			} `json:"package"`
		} `json:"objects"`
	}

	if err := json.Unmarshal(body, &npmResp); err != nil {
		return resp, fmt.Errorf("failed to parse npm response: %w", err)
	}

	for _, obj := range npmResp.Objects {
		pkg := obj.Package
		pkgURL := pkg.Links.NPM
		if pkgURL == "" {
			pkgURL = fmt.Sprintf("https://www.npmjs.com/package/%s", pkg.Name)
		}
		resp.Results = append(resp.Results, packageInfo{
			Name:        pkg.Name,
			Version:     pkg.Version,
			Description: p.truncateStr(pkg.Description, 200),
			URL:         pkgURL,
		})
	}

	resp.Count = len(resp.Results)
	resp.Message = fmt.Sprintf("Found %d npm package(s) for %q", resp.Count, query)
	return resp, nil
}

// ────────────────────── PyPI (pypi.org) ──────────────────────

func (p *pkgTools) searchPyPI(ctx context.Context, query string, resp searchResponse) (searchResponse, error) {
	// PyPI doesn't have a search API, but we can do an exact package lookup.
	apiURL := fmt.Sprintf("%s/pypi/%s/json", p.pypiBaseURL, url.PathEscape(query))

	body, err := p.doGet(ctx, apiURL)
	if err != nil {
		// If exact match fails, try the simple API search.
		resp.Message = fmt.Sprintf("Package %q not found on PyPI. Try browsing: https://pypi.org/search/?q=%s",
			query, url.QueryEscape(query))
		resp.Count = 0
		return resp, nil
	}

	var pypiResp struct {
		Info struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			Summary    string `json:"summary"`
			ProjectURL string `json:"project_url"`
			PackageURL string `json:"package_url"`
			HomePage   string `json:"home_page"`
		} `json:"info"`
	}

	if err := json.Unmarshal(body, &pypiResp); err != nil {
		return resp, fmt.Errorf("failed to parse PyPI response: %w", err)
	}

	pkgURL := pypiResp.Info.PackageURL
	if pkgURL == "" {
		pkgURL = pypiResp.Info.ProjectURL
	}

	resp.Results = append(resp.Results, packageInfo{
		Name:        pypiResp.Info.Name,
		Version:     pypiResp.Info.Version,
		Description: p.truncateStr(pypiResp.Info.Summary, 200),
		URL:         pkgURL,
	})

	resp.Count = len(resp.Results)
	resp.Message = fmt.Sprintf("Found PyPI package %q v%s", pypiResp.Info.Name, pypiResp.Info.Version)
	return resp, nil
}

// ────────────────────── Helpers ──────────────────────

func (p *pkgTools) doGet(ctx context.Context, apiURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, apiURL)
	}

	return io.ReadAll(resp.Body)
}

func (p *pkgTools) truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
