// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticrouter

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/rbac"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// CacheToolName is the tool name for the semantic cache management tool.
const CacheToolName = "semantic_cache"

// CacheToolRequest is the input for the semantic_cache tool.
type CacheToolRequest struct {
	Action string `json:"action" jsonschema:"description=What to do: 'search' (find cache entries by query text) or 'delete' (delete specific entries by ID) or 'clear_all' (remove all cache entries),required,enum=search,enum=delete,enum=clear_all"`

	// Fields for action=search
	Query string `json:"query,omitempty" jsonschema:"description=Search query text to find similar cached entries (required for action=search)"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Max results to return (default 20 and max 50). Applies to action=search"`

	// Fields for action=delete
	IDs []string `json:"ids,omitempty" jsonschema:"description=Array of cache entry IDs to delete (required for action=delete). Get IDs from a search result first"`
}

// CacheToolResponse is the output for the semantic_cache tool.
type CacheToolResponse struct {
	Message string       `json:"message"`
	Entries []CacheEntry `json:"entries,omitempty"`
	Count   int          `json:"count"`
}

// cacheTool wraps an IRouter to provide cache management functionality.
type cacheTool struct {
	router IRouter
	rbac   *rbac.RBAC
}

// NewCacheTool creates the semantic_cache management tool.
// Pass a non-nil IRouter; returns nil when router is nil.
// The rbac parameter controls which users can perform destructive actions
// (delete, clear_all). When nil, all actions are unrestricted.
func NewCacheTool(router IRouter, rbac *rbac.RBAC) tool.Tool {
	if router == nil {
		return nil
	}
	t := &cacheTool{router: router, rbac: rbac}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(CacheToolName),
		function.WithDescription(
			"Manage the semantic cache. The semantic cache stores previous Q&A pairs "+
				"and uses them as hints for future similar questions. "+
				"Use action='search' with a query to find cached entries. "+
				"Use action='delete' with IDs to remove specific entries (admin only). "+
				"Use action='clear_all' to remove all cached entries (admin only). "+
				"Example: {\"action\":\"search\",\"query\":\"container image report\",\"limit\":10}",
		),
	)
}

const maxCacheSearchLimit = 50

// execute routes the semantic_cache request to the appropriate handler.
func (t *cacheTool) execute(ctx context.Context, req CacheToolRequest) (CacheToolResponse, error) {
	switch req.Action {
	case "search":
		return t.search(ctx, req)
	case "delete":
		return t.deleteCacheEntries(ctx, req)
	case "clear_all":
		return t.clearAll(ctx)
	default:
		return CacheToolResponse{}, fmt.Errorf("action must be 'search', 'delete', or 'clear_all', got %q", req.Action)
	}
}

// requireAdmin checks RBAC and returns an error if the user is not an admin.
func (t *cacheTool) requireAdmin(ctx context.Context, action string) error {
	if t.rbac != nil && !t.rbac.IsAdmin(ctx) {
		return fmt.Errorf("permission denied: action %q requires admin privileges", action)
	}
	return nil
}

func (t *cacheTool) search(ctx context.Context, req CacheToolRequest) (CacheToolResponse, error) {
	if req.Query == "" {
		return CacheToolResponse{}, fmt.Errorf("query is required for action=search")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > maxCacheSearchLimit {
		limit = maxCacheSearchLimit
	}

	entries, err := t.router.SearchCache(ctx, req.Query, limit)
	if err != nil {
		return CacheToolResponse{}, err
	}

	return CacheToolResponse{
		Message: fmt.Sprintf("Found %d cache entries matching query", len(entries)),
		Entries: entries,
		Count:   len(entries),
	}, nil
}

func (t *cacheTool) deleteCacheEntries(ctx context.Context, req CacheToolRequest) (CacheToolResponse, error) {
	if err := t.requireAdmin(ctx, "delete"); err != nil {
		return CacheToolResponse{}, err
	}

	if len(req.IDs) == 0 {
		return CacheToolResponse{}, fmt.Errorf("ids array is required and must not be empty for action=delete")
	}

	count, err := t.router.DeleteCacheEntries(ctx, req.IDs)
	if err != nil {
		return CacheToolResponse{}, err
	}

	return CacheToolResponse{
		Message: fmt.Sprintf("Deleted %d cache entries", count),
		Count:   count,
	}, nil
}

func (t *cacheTool) clearAll(ctx context.Context) (CacheToolResponse, error) {
	if err := t.requireAdmin(ctx, "clear_all"); err != nil {
		return CacheToolResponse{}, err
	}

	count, err := t.router.ClearCache(ctx)
	if err != nil {
		return CacheToolResponse{}, err
	}

	return CacheToolResponse{
		Message: fmt.Sprintf("Cleared all %d cache entries", count),
		Count:   count,
	}, nil
}
