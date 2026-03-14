// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	genieLogger "github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"golang.org/x/sync/errgroup"

	"github.com/stackgenhq/genie/pkg/datasource"
)

// mcpResourceClient is the minimal interface the datasource layer needs from
// an MCP connection. *client.Client satisfies this.
type mcpResourceClient interface {
	ListResources(ctx context.Context, req mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error)
	ReadResource(ctx context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
	io.Closer
}

// datasourceDialer abstracts ephemeral MCP client creation so tests can inject
// a fake without hitting real transports.
type datasourceDialer func(ctx context.Context, cfg MCPServerConfig, sp security.SecretProvider) (mcpResourceClient, error)

// defaultDatasourceDialer wraps dialMCP and returns the concrete *client.Client.
func defaultDatasourceDialer(ctx context.Context, cfg MCPServerConfig, sp security.SecretProvider) (mcpResourceClient, error) {
	return dialMCP(ctx, cfg, sp)
}

// RegisterDatasources registers a ConnectorFactory for each configured MCP
// server so that the datasource sync pipeline can enumerate and vectorize
// MCP resources.
func (c *Client) RegisterDatasources() []datasource.DataSource {
	for _, server := range c.config.Servers {
		server := server // capture loop variable
		datasource.RegisterConnectorFactory(server.Name, func(ctx context.Context, cfg datasource.ConnectorOptions) datasource.DataSource {
			return &mcpDatasource{
				cfg:            server,
				secretProvider: c.secretProvider,
				dial:           defaultDatasourceDialer,
			}
		})
	}
	return nil
}

// mcpDatasource implements datasource.DataSource by reading MCP resources from
// a configured MCP server. It creates an ephemeral MCP client connection,
// lists all resources via ListResources (paginated), optionally filters by
// updated-since, and reads each via ReadResource, converting
// TextResourceContents to NormalizedItems. Binary resources
// (BlobResourceContents) are skipped.
type mcpDatasource struct {
	cfg            MCPServerConfig
	secretProvider security.SecretProvider
	dial           datasourceDialer
}

// Name returns the source identifier (e.g. "jira", "confluence", "servicenow").
func (m *mcpDatasource) Name() string {
	return m.cfg.Name
}

// ListItems lists all MCP resources from the configured server, reads each
// resource, and converts TextResourceContents into NormalizedItems. Binary
// resources are skipped. Errors reading individual resources are logged and
// skipped (partial results).
func (m *mcpDatasource) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	return m.listItemsWithSince(ctx, scope, time.Time{})
}

// ListItemsSince returns only items whose lastModified annotation is after
// the given time. Resources without a lastModified annotation are always
// included (we can't know their age, so we err on the side of inclusion).
func (m *mcpDatasource) ListItemsSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	return m.listItemsWithSince(ctx, scope, since)
}

// listItemsWithSince is the shared implementation for ListItems and
// ListItemsSince. When since is zero, no time-based filtering is applied.
// When DatasourceKeywordRegex is configured, only resources whose URI or
// Name matches at least one pattern are included.
func (m *mcpDatasource) listItemsWithSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	log := genieLogger.GetLogger(ctx).With("fn", "mcpDatasource.ListItems", "source", m.cfg.Name)

	mcpClient, err := m.dial(ctx, m.cfg, m.secretProvider)
	if err != nil {
		return nil, fmt.Errorf("%s: dial MCP: %w", m.cfg.Name, err)
	}
	defer mcpClient.Close()

	// List all resources (paginated)
	resources, err := m.listAllResources(ctx, mcpClient)
	if err != nil {
		return nil, fmt.Errorf("%s: list resources: %w", m.cfg.Name, err)
	}

	if len(resources) == 0 {
		return nil, nil
	}

	// Compile keyword regex patterns (if configured) for resource filtering.
	keywordPatterns := m.compileKeywordPatterns(ctx)

	var (
		mu  sync.Mutex
		out []datasource.NormalizedItem
	)

	g, gctx := errgroup.WithContext(ctx)
	// Bound concurrency to avoid overwhelming the MCP server
	g.SetLimit(10)

	for _, res := range resources {
		// Apply keyword regex filter: skip resources that don't match any pattern.
		if len(keywordPatterns) > 0 && !m.matchesKeywordPatterns(res, keywordPatterns) {
			continue
		}

		// Apply time-based filter: skip resources with known lastModified
		// that is before the since threshold.
		if !since.IsZero() && res.Annotations != nil && res.Annotations.LastModified != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, res.Annotations.LastModified); parseErr == nil {
				if parsed.Before(since) {
					continue
				}
			}
		}

		res := res // capture loop variable
		g.Go(func() error {
			items, readErr := m.readResource(gctx, mcpClient, res)
			if readErr != nil {
				log.Warn("failed to read resource, skipping", "uri", res.URI, "error", readErr)
				return nil // continue processing other resources
			}
			if len(items) > 0 {
				mu.Lock()
				out = append(out, items...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("%s: read resources: %w", m.cfg.Name, err)
	}

	return out, nil
}

// compileKeywordPatterns compiles DatasourceKeywordRegex patterns from config.
// Invalid patterns are logged and skipped.
func (m *mcpDatasource) compileKeywordPatterns(ctx context.Context) []*regexp.Regexp {
	if len(m.cfg.DatasourceKeywordRegex) == 0 {
		return nil
	}
	log := genieLogger.GetLogger(ctx).With("fn", "mcpDatasource.compileKeywordPatterns", "source", m.cfg.Name)
	var patterns []*regexp.Regexp
	for _, raw := range m.cfg.DatasourceKeywordRegex {
		re, err := regexp.Compile(raw)
		if err != nil {
			log.Warn("invalid datasource keyword regex, skipping", "pattern", raw, "error", err)
			continue
		}
		patterns = append(patterns, re)
	}
	return patterns
}

// matchesKeywordPatterns returns true if any compiled pattern matches the
// resource URI or Name.
func (m *mcpDatasource) matchesKeywordPatterns(res mcp.Resource, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(res.URI) || re.MatchString(res.Name) {
			return true
		}
	}
	return false
}

// listAllResources paginates through ListResources and returns all resources.
// Max 100 pages to prevent unbounded requests.
func (m *mcpDatasource) listAllResources(ctx context.Context, reader mcpResourceClient) ([]mcp.Resource, error) {
	var allResources []mcp.Resource
	var cursor mcp.Cursor

	for page := 0; page < 100; page++ {
		req := mcp.ListResourcesRequest{}
		if cursor != "" {
			req.Params.Cursor = cursor
		}

		resp, err := reader.ListResources(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			break
		}

		allResources = append(allResources, resp.Resources...)

		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}

	if cursor != "" {
		genieLogger.GetLogger(ctx).Warn("mcpDatasource: list resources paginated to maximum depth. Results truncated.", "source", m.cfg.Name, "pages", 100)
	}

	return allResources, nil
}

// readResource reads a single MCP resource and converts its text contents to
// NormalizedItems. Binary (blob) contents are skipped.
func (m *mcpDatasource) readResource(ctx context.Context, reader mcpResourceClient, res mcp.Resource) ([]datasource.NormalizedItem, error) {
	resp, err := reader.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: res.URI,
		},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}

	var items []datasource.NormalizedItem
	for _, contentItem := range resp.Contents {
		textContent, ok := contentItem.(mcp.TextResourceContents)
		if !ok {
			continue // Skip binary/blob content
		}
		if textContent.Text == "" {
			continue
		}

		meta := map[string]string{
			"uri": textContent.URI,
		}
		if res.Name != "" {
			meta["title"] = res.Name
		}
		if res.MIMEType != "" {
			meta["mime_type"] = res.MIMEType
		}
		if res.Description != "" {
			meta["description"] = res.Description
		}

		updatedAt := time.Now()
		if res.Annotations != nil && res.Annotations.LastModified != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, res.Annotations.LastModified); parseErr == nil {
				updatedAt = parsed
			}
		}

		content := textContent.Text
		if res.Name != "" && res.Name != textContent.Text {
			content = res.Name + "\n\n" + textContent.Text
		}

		items = append(items, datasource.NormalizedItem{
			ID:     fmt.Sprintf("%s:%s", m.cfg.Name, res.URI),
			Source: m.cfg.Name,
			SourceRef: &datasource.SourceRef{
				Type:  m.cfg.Name,
				RefID: res.URI,
			},
			UpdatedAt: updatedAt,
			Content:   content,
			Metadata:  meta,
		})
	}

	return items, nil
}

// Ensure mcpDatasource implements datasource.DataSource and
// datasource.ListItemsSince at compile time.
var (
	_ datasource.DataSource     = (*mcpDatasource)(nil)
	_ datasource.ListItemsSince = (*mcpDatasource)(nil)
)
