// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

// toolIndexMetaType is the metadata value used to tag tool declarations in
// the vector store, preventing collisions with memory/graph documents.
const toolIndexMetaType = "tool_index"

// ToolSearchResult represents a tool found via semantic search.
type ToolSearchResult struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// VectorToolProvider indexes tool declarations into a vector store and
// provides semantic search over them. This enables the orchestrator to
// find relevant tools by goal description instead of listing all tools
// in prompts — reducing hallucination and prompt bloat.
//
// Important: this is a **lookup service only**. It does NOT implement
// the ToolProviders interface and never gives tools to agents directly.
// Sub-agents receive concrete tools via Registry.Include().
type VectorToolProvider struct {
	store vector.IStore
}

// NewVectorToolProvider creates a VectorToolProvider and indexes every tool
// from the given registry into the vector store. Each tool is stored as a
// document with ID "tool:<name>", text "<name>: <description>", and
// metadata type=tool_index.
//
// The indexing is idempotent (upsert) so repeated calls with the same
// registry are safe.
func NewVectorToolProvider(ctx context.Context, store vector.IStore, registry *Registry) (*VectorToolProvider, error) {
	if store == nil {
		return nil, fmt.Errorf("vector store is nil; cannot create VectorToolProvider")
	}

	vtp := &VectorToolProvider{store: store}

	if err := vtp.indexTools(ctx, registry); err != nil {
		return nil, fmt.Errorf("failed to index tools: %w", err)
	}

	return vtp, nil
}

// indexTools upserts all tool declarations from the registry into the
// vector store. Uses batch upsert for efficiency.
func (v *VectorToolProvider) indexTools(ctx context.Context, registry *Registry) error {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.indexTools")

	allTools := registry.GetTools()
	if len(allTools) == 0 {
		logr.Info("no tools to index")
		return nil
	}

	items := make([]vector.BatchItem, 0, len(allTools))
	for _, t := range allTools {
		decl := t.Declaration()
		items = append(items, vector.BatchItem{
			ID:   "tool:" + decl.Name,
			Text: decl.Name + ": " + decl.Description,
			Metadata: map[string]string{
				"type":      toolIndexMetaType,
				"tool_name": decl.Name,
			},
		})
	}

	if err := v.store.Upsert(ctx, vector.UpsertRequest{Items: items}); err != nil {
		return fmt.Errorf("failed to upsert tool index: %w", err)
	}

	logr.Info("tool index built", "count", len(items))
	return nil
}

// SearchTools performs a semantic search over indexed tool declarations,
// returning tools whose descriptions are most relevant to the query.
// Results are ordered by similarity score (highest first).
func (v *VectorToolProvider) SearchTools(ctx context.Context, query string, limit int) ([]ToolSearchResult, error) {
	if v.store == nil {
		return nil, nil
	}

	if limit <= 0 {
		limit = 10
	}

	results, err := v.store.Search(ctx, vector.SearchRequest{
		Query: query,
		Limit: limit,
		Filter: map[string]string{
			"type": toolIndexMetaType,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("tool index search failed: %w", err)
	}

	toolResults := make([]ToolSearchResult, 0, len(results))
	for _, r := range results {
		name := r.Metadata["tool_name"]
		if name == "" {
			continue
		}

		// Extract description from content (format: "name: description")
		desc := r.Content
		if idx := strings.Index(desc, ": "); idx >= 0 {
			desc = desc[idx+2:]
		}

		toolResults = append(toolResults, ToolSearchResult{
			Name:        name,
			Description: desc,
			Score:       r.Score,
		})
	}

	return toolResults, nil
}

// FormatToolList formats search results as a concise string suitable for
// inclusion in LLM prompts. Each tool is listed as "- name: description".
func (v *VectorToolProvider) FormatToolList(results []ToolSearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString("- ")
		sb.WriteString(r.Name)
		sb.WriteString(": ")
		sb.WriteString(r.Description)
		sb.WriteString("\n")
	}
	return sb.String()
}
