// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

// toolIndexMetaType is the metadata value used to tag tool declarations in
// the vector store, preventing collisions with memory/graph documents.
const toolIndexMetaType = "tool_index"

// Blended scoring weights for SearchToolsWithContext.
// Semantic similarity dominates so new/unknown tools still surface.
// Co-occurrence provides a boost so frequently-paired tools rank higher.
const (
	semanticWeight     = 0.7
	cooccurrenceWeight = 0.3
)

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
// It also maintains an in-memory co-occurrence graph that learns which
// tools are commonly used together (AutoTool-style). This graph is built
// incrementally from TreeResult.ToolCallCounts after each sub-agent run.
//
// Important: this is a **lookup service only**. It does NOT implement
// the ToolProviders interface and never gives tools to agents directly.
// Sub-agents receive concrete tools via Registry.Include().
type VectorToolProvider struct {
	store vector.IStore

	// cooccurrence tracks pairwise tool usage. cooccurrence[A][B] = N
	// means tools A and B appeared together in N task runs. The graph
	// is symmetric: recording {A, B} increments both [A][B] and [B][A].
	cooccurrence map[string]map[string]int

	// maxEdgeWeight tracks the highest edge weight seen so far.
	// Used to normalize CooccurrenceScore to [0, 1].
	maxEdgeWeight int

	mu sync.RWMutex
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

	vtp := &VectorToolProvider{
		store:        store,
		cooccurrence: make(map[string]map[string]int),
	}

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

	return v.parseSearchResults(results), nil
}

// SearchToolsWithContext performs a semantic search and then re-ranks results
// using co-occurrence affinity with the provided context tools. The blended
// score is: 0.7*semantic + 0.3*max(cooccurrence with each contextTool).
//
// This makes tool recommendations context-aware: tools that are commonly
// used alongside the context tools rank higher. If the co-occurrence graph
// is empty (cold start), results are ranked purely by semantic similarity.
func (v *VectorToolProvider) SearchToolsWithContext(ctx context.Context, query string, contextTools []string, limit int) ([]ToolSearchResult, error) {
	if v.store == nil {
		return nil, nil
	}

	if limit <= 0 {
		limit = 10
	}

	// Fetch more candidates than needed so re-ranking has a good pool.
	fetchLimit := limit * 3
	if fetchLimit < 20 {
		fetchLimit = 20
	}

	results, err := v.store.Search(ctx, vector.SearchRequest{
		Query: query,
		Limit: fetchLimit,
		Filter: map[string]string{
			"type": toolIndexMetaType,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("tool index search failed: %w", err)
	}

	parsed := v.parseSearchResults(results)

	// If no context tools or empty graph, return pure semantic results.
	v.mu.RLock()
	hasGraph := v.maxEdgeWeight > 0
	v.mu.RUnlock()

	if len(contextTools) == 0 || !hasGraph {
		if len(parsed) > limit {
			parsed = parsed[:limit]
		}
		return parsed, nil
	}

	// Re-rank using blended scoring.
	for i := range parsed {
		semanticScore := parsed[i].Score

		// Find the max co-occurrence score with any context tool.
		var maxCooc float64
		for _, ct := range contextTools {
			s := v.CooccurrenceScore(parsed[i].Name, ct)
			if s > maxCooc {
				maxCooc = s
			}
		}

		parsed[i].Score = semanticWeight*semanticScore + cooccurrenceWeight*maxCooc
	}

	// Re-sort by blended score.
	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].Score > parsed[j].Score
	})

	if len(parsed) > limit {
		parsed = parsed[:limit]
	}
	return parsed, nil
}

// RecordToolUsage records that the given tools were used together in a
// single task run. This updates the co-occurrence graph by incrementing
// all pairwise edges. The graph is symmetric.
//
// Called after every sub-agent completes with its TreeResult.ToolCallCounts.
func (v *VectorToolProvider) RecordToolUsage(toolNames []string) {
	if len(toolNames) < 2 {
		return // Need at least 2 tools for co-occurrence.
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	for i := 0; i < len(toolNames); i++ {
		for j := i + 1; j < len(toolNames); j++ {
			a, b := toolNames[i], toolNames[j]

			if v.cooccurrence[a] == nil {
				v.cooccurrence[a] = make(map[string]int)
			}
			if v.cooccurrence[b] == nil {
				v.cooccurrence[b] = make(map[string]int)
			}

			v.cooccurrence[a][b]++
			v.cooccurrence[b][a]++

			// Track max for normalization.
			if v.cooccurrence[a][b] > v.maxEdgeWeight {
				v.maxEdgeWeight = v.cooccurrence[a][b]
			}
		}
	}
}

// CooccurrenceScore returns a normalized affinity score [0, 1] between
// two tools based on how frequently they appear together. Returns 0 if
// either tool is unknown or the graph is empty.
func (v *VectorToolProvider) CooccurrenceScore(toolA, toolB string) float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.maxEdgeWeight == 0 {
		return 0
	}

	edges := v.cooccurrence[toolA]
	if edges == nil {
		return 0
	}

	weight := edges[toolB]
	if weight == 0 {
		return 0
	}

	// Log-normalized score to avoid linearly scaling with outlier counts.
	// log(1+w) / log(1+max) gives [0, 1] with diminishing returns.
	return math.Log1p(float64(weight)) / math.Log1p(float64(v.maxEdgeWeight))
}

// parseSearchResults converts raw vector search results into ToolSearchResult
// structs, filtering out entries without a tool_name.
func (v *VectorToolProvider) parseSearchResults(results []vector.SearchResult) []ToolSearchResult {
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
	return toolResults
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
