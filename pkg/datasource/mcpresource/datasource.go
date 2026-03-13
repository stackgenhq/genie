// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package mcpresource provides a DataSource connector that reads MCP resources
// and converts them to NormalizedItems for vectorization. It bridges the MCP
// resource protocol (ListResources + ReadResource) with the datasource.DataSource
// interface so that any MCP server exposing resources (Jira, Confluence,
// ServiceNow, etc.) can be used as a data source without bespoke API clients.
package mcpresource

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	genieLogger "github.com/stackgenhq/genie/pkg/logger"
	"golang.org/x/sync/errgroup"

	"github.com/stackgenhq/genie/pkg/datasource"
)

// Reader is the subset of the MCP client interface used for listing and
// reading resources. Defined here to avoid a direct dependency on pkg/mcp,
// which would create an import cycle with datasource sub-packages.
// *mcp.Client from pkg/mcp satisfies this interface.
type Reader interface {
	ListResources(ctx context.Context, req mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error)
	ReadResource(ctx context.Context, req mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
}

// ScopeFilter is a function that decides whether a resource matches the
// configured scope. Connectors inject their own filter based on scope fields
// (e.g. Jira filters by ProjectKeys matching URI prefixes). When nil, all
// resources are included.
type ScopeFilter func(res mcp.Resource, scope datasource.Scope) bool

// MCPResourceConnector implements datasource.DataSource by reading MCP resources
// from a configured MCP server. It lists all resources via ListResources
// (paginated), optionally filters by scope and updated-since, and reads each
// via ReadResource, converting TextResourceContents to NormalizedItems.
// Binary resources (BlobResourceContents) are skipped.
type MCPResourceConnector struct {
	reader      Reader
	sourceName  string
	scopeFilter ScopeFilter
}

// Option configures an MCPResourceConnector.
type Option func(*MCPResourceConnector)

// WithScopeFilter sets a function that filters listed MCP resources based on
// the datasource.Scope passed to ListItems. Only resources for which the
// filter returns true are read and converted. When not set, all resources
// are included.
func WithScopeFilter(fn ScopeFilter) Option {
	return func(c *MCPResourceConnector) {
		c.scopeFilter = fn
	}
}

// NewMCPResourceConnector returns a DataSource that reads MCP resources.
// sourceName is the datasource identifier (e.g. "jira", "confluence", "servicenow").
// reader is the Reader for the target MCP server.
func NewMCPResourceConnector(reader Reader, sourceName string, opts ...Option) *MCPResourceConnector {
	c := &MCPResourceConnector{
		reader:     reader,
		sourceName: sourceName,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the source identifier (e.g. "jira", "confluence", "servicenow").
func (c *MCPResourceConnector) Name() string {
	return c.sourceName
}

// ListItems lists all MCP resources from the configured server, filters by
// scope when a ScopeFilter is set, reads each resource, and converts
// TextResourceContents into NormalizedItems. Binary resources are skipped.
// Errors reading individual resources are logged and skipped (partial results).
func (c *MCPResourceConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	return c.listItemsWithSince(ctx, scope, time.Time{})
}

// ListItemsSince returns only items whose lastModified annotation is after
// the given time. Resources without a lastModified annotation are always
// included (we can't know their age, so we err on the side of inclusion).
func (c *MCPResourceConnector) ListItemsSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	return c.listItemsWithSince(ctx, scope, since)
}

// listItemsWithSince is the shared implementation for ListItems and
// ListItemsSince. When since is zero, no time-based filtering is applied.
func (c *MCPResourceConnector) listItemsWithSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	if c.reader == nil {
		return nil, fmt.Errorf("%s: MCP resource reader is nil", c.sourceName)
	}

	log := genieLogger.GetLogger(ctx).With("fn", "mcpresource.ListItems", "source", c.sourceName)

	// List all resources (paginated)
	resources, err := c.listAllResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: list resources: %w", c.sourceName, err)
	}

	if len(resources) == 0 {
		return nil, nil
	}

	var (
		mu  sync.Mutex
		out []datasource.NormalizedItem
	)

	g, gctx := errgroup.WithContext(ctx)
	// Bound concurrency to avoid overwhelming the MCP server
	g.SetLimit(10)

	for _, res := range resources {
		// Apply scope filter if configured
		if c.scopeFilter != nil && !c.scopeFilter(res, scope) {
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
			items, err := c.readResource(gctx, res)
			if err != nil {
				log.Warn("failed to read resource, skipping", "uri", res.URI, "error", err)
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
		return nil, fmt.Errorf("%s: read resources: %w", c.sourceName, err)
	}

	return out, nil
}

// listAllResources paginates through ListResources and returns all resources.
// Max 100 pages to prevent unbounded requests.
func (c *MCPResourceConnector) listAllResources(ctx context.Context) ([]mcp.Resource, error) {
	var allResources []mcp.Resource
	var cursor mcp.Cursor

	for page := 0; page < 100; page++ {
		req := mcp.ListResourcesRequest{}
		if cursor != "" {
			req.Params.Cursor = cursor
		}

		resp, err := c.reader.ListResources(ctx, req)
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

	return allResources, nil
}

// readResource reads a single MCP resource and converts its text contents to
// NormalizedItems. Binary (blob) contents are skipped.
func (c *MCPResourceConnector) readResource(ctx context.Context, res mcp.Resource) ([]datasource.NormalizedItem, error) {
	resp, err := c.reader.ReadResource(ctx, mcp.ReadResourceRequest{
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
	for _, content := range resp.Contents {
		textContent, ok := content.(mcp.TextResourceContents)
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
			ID:     fmt.Sprintf("%s:%s", c.sourceName, res.URI),
			Source: c.sourceName,
			SourceRef: &datasource.SourceRef{
				Type:  c.sourceName,
				RefID: res.URI,
			},
			UpdatedAt: updatedAt,
			Content:   content,
			Metadata:  meta,
		})
	}

	return items, nil
}

// Ensure MCPResourceConnector implements datasource.DataSource and
// datasource.ListItemsSince at compile time.
var (
	_ datasource.DataSource     = (*MCPResourceConnector)(nil)
	_ datasource.ListItemsSince = (*MCPResourceConnector)(nil)
)
