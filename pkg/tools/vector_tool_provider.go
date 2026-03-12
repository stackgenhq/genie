// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/graph"
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

// toolIndexMetaType is the metadata value used to tag tool declarations in
// the vector store, preventing collisions with memory/graph documents.
const toolIndexMetaType = "tool_index"

// cooccurrenceEntityType is the entity type for tools stored in the graph
// store for co-occurrence tracking.
const cooccurrenceEntityType = "cooccurrence_tool"

// cooccurrenceAttrPrefix prefixes entity attrs keys that encode edge weights.
// Format: "cooc:<other_tool>" → "<count>".
const cooccurrenceAttrPrefix = "cooc:"

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

type ToolSearchResults []ToolSearchResult

//counterfeiter:generate . SmartToolProvider

// SmartToolProvider is a tool provider that can perform semantic search and
// co-occurrence analysis to find relevant tools.
type SmartToolProvider interface {
	// SearchToolsWithContext performs a semantic search and then re-ranks results
	// using co-occurrence affinity with the provided context tools. The blended
	// score is: 0.7*semantic + 0.3*max(cooccurrence with each contextTool).
	SearchToolsWithContext(ctx context.Context, query string, contextTools []string, limit int) (ToolSearchResults, error)

	// RecordToolUsage records that the given tools were used together in a
	// single task run. This updates the co-occurrence graph by incrementing
	// all pairwise edges. The graph is symmetric.
	RecordToolUsage(ctx context.Context, toolNames []string)
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

	// graphStore persists co-occurrence data across restarts. When
	// non-nil, each tool is stored as a graph.Entity with type
	// "cooccurrence_tool" and edge weights encoded in Attrs.
	// When nil, the co-occurrence graph is ephemeral (in-memory only).
	graphStore graph.IStore

	// DisableCooccurrenceCache when true skips the in-memory map and reads
	// co-occurrence scores directly from the graph store. Useful when the
	// graph store is fast (e.g. in-memory graph) and you want to avoid
	// duplicate state. Has no effect when graphStore is nil.
	DisableCooccurrenceCache bool

	// indexedToolIDs tracks which tool IDs ("tool:<name>") were last
	// indexed into the vector store. On re-index, any IDs not in the
	// new registry are deleted to prevent stale/denied tools from
	// appearing in search results.
	indexedToolIDs map[string]struct{}

	// cooccurrence tracks pairwise tool usage. cooccurrence[A][B] = N
	// means tools A and B appeared together in N task runs. The graph
	// is symmetric: recording {A, B} increments both [A][B] and [B][A].
	// Skipped when DisableCooccurrenceCache is true.
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
func NewVectorToolProvider(ctx context.Context, store vector.IStore, registry *Registry, graphStore graph.IStore) (*VectorToolProvider, error) {
	logr := logger.GetLogger(ctx).With("fn", "NewVectorToolProvider")

	if store == nil || graphStore == nil {
		return nil, fmt.Errorf("vector store or graph store is nil; cannot create VectorToolProvider")
	}

	vtp := &VectorToolProvider{
		store:        store,
		graphStore:   graphStore,
		cooccurrence: make(map[string]map[string]int),
	}

	if err := vtp.indexTools(ctx, registry); err != nil {
		return nil, fmt.Errorf("failed to index tools: %w", err)
	}

	// Hydrate co-occurrence graph from persistent storage.
	logr.Info("loading co-occurrence graph from graph store")
	if err := vtp.loadCooccurrence(ctx); err != nil {
		logr.Warn("failed to load co-occurrence graph from graph store, starting fresh", "error", err)
	}

	return vtp, nil
}

// indexTools upserts all tool declarations from the registry into the
// vector store, then prunes stale entries for tools that are no longer
// in the registry (e.g. because they were denied in .genie.toml).
func (v *VectorToolProvider) indexTools(ctx context.Context, registry *Registry) error {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.indexTools")

	allTools := registry.GetTools()
	if len(allTools) == 0 {
		logr.Info("no tools to index")
		return nil
	}

	// Build batch items and track current IDs.
	newIDs := make(map[string]struct{}, len(allTools))
	items := make([]vector.BatchItem, 0, len(allTools))
	for _, t := range allTools {
		decl := t.Declaration()
		id := "tool:" + decl.Name
		newIDs[id] = struct{}{}
		items = append(items, vector.BatchItem{
			ID:   id,
			Text: decl.Name + ": " + decl.Description,
			Metadata: map[string]string{
				"type":      toolIndexMetaType,
				"tool_name": decl.Name,
			},
		})
	}

	// Prune stale entries: delete tool IDs that were previously indexed
	// but are absent from the current registry (e.g. newly denied tools).
	if v.indexedToolIDs != nil {
		var staleIDs []string
		for oldID := range v.indexedToolIDs {
			if _, ok := newIDs[oldID]; !ok {
				staleIDs = append(staleIDs, oldID)
			}
		}
		if len(staleIDs) > 0 {
			logr.Info("pruning stale tool entries from vector store",
				"stale_count", len(staleIDs), "stale", staleIDs)
			if err := v.store.Delete(ctx, vector.DeleteRequest{IDs: staleIDs}); err != nil {
				logr.Warn("failed to prune stale tool entries", "error", err)
				// Non-fatal: continue with upsert.
			}
		}
	}
	v.indexedToolIDs = newIDs

	if err := v.store.Upsert(ctx, vector.UpsertRequest{Items: items}); err != nil {
		return fmt.Errorf("failed to upsert tool index: %w", err)
	}

	logr.Info("tool index built", "count", len(items))
	return nil
}

// SearchTools performs a semantic search over indexed tool declarations,
// returning tools whose descriptions are most relevant to the query.
// Results are ordered by similarity score (highest first).
func (v *VectorToolProvider) SearchTools(ctx context.Context, query string, limit int) (ToolSearchResults, error) {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.SearchTools")

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
		logr.Warn("tool index search failed", "query", query, "error", err)
		return nil, fmt.Errorf("tool index search failed: %w", err)
	}

	parsed := v.parseSearchResults(results)
	logr.Info("tool search completed", "query", query, "limit", limit, "results", len(parsed))
	return parsed, nil
}

// SearchToolsWithContext performs a semantic search and then re-ranks results
// using co-occurrence affinity with the provided context tools. The blended
// score is: 0.7*semantic + 0.3*max(cooccurrence with each contextTool).
//
// This makes tool recommendations context-aware: tools that are commonly
// used alongside the context tools rank higher. If the co-occurrence graph
// is empty (cold start), results are ranked purely by semantic similarity.
func (v *VectorToolProvider) SearchToolsWithContext(ctx context.Context, query string, contextTools []string, limit int) (ToolSearchResults, error) {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.SearchToolsWithContext")

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
		logr.Warn("tool index search failed", "query", query, "error", err)
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
		logr.Info("returning pure semantic results (no co-occurrence data)", "query", query, "results", len(parsed))
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

	logr.Info("blended search completed", "query", query, "context_tools", len(contextTools), "results", len(parsed), "maxEdgeWeight", v.maxEdgeWeight)
	return parsed, nil
}

// RecordToolUsage records that the given tools were used together in a
// single task run. This updates the co-occurrence graph by incrementing
// all pairwise edges. The graph is symmetric.
//
// Called after every sub-agent completes with its TreeResult.ToolCallCounts.
func (v *VectorToolProvider) RecordToolUsage(ctx context.Context, toolNames []string) {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.RecordToolUsage")

	if len(toolNames) < 2 {
		logr.Info("not enough tools to record co-occurrence", "tool_names", toolNames)
		return // Need at least 2 tools for co-occurrence.
	}

	// Track which tools changed so we only persist dirty entities.
	changed := make(map[string]struct{})

	v.mu.Lock()
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

			changed[a] = struct{}{}
			changed[b] = struct{}{}

			// Track max for normalization.
			if v.cooccurrence[a][b] > v.maxEdgeWeight {
				v.maxEdgeWeight = v.cooccurrence[a][b]
			}
		}
	}
	v.mu.Unlock()

	// Persist changed entities to graph store (best-effort, outside lock).
	v.persistCooccurrence(ctx, changed)
}

// loadCooccurrence hydrates the in-memory co-occurrence map from entities
// stored in the graph store. Each entity of type "cooccurrence_tool" has
// attrs like "cooc:write_file" → "5" encoding pairwise counts.
func (v *VectorToolProvider) loadCooccurrence(ctx context.Context) error {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.loadCooccurrence")

	// We need to discover all cooccurrence_tool entities. The graph store
	// doesn't have a "list all by type" API, but we can look up entities
	// by ID. We use a sentinel entity that records known tool names.
	sentinel, err := v.graphStore.GetEntity(ctx, "cooccurrence:index")
	if err != nil {
		return fmt.Errorf("failed to get co-occurrence index: %w", err)
	}
	if sentinel == nil {
		logr.Info("no co-occurrence index found, starting fresh")
		return nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	toolNames := make([]string, 0, len(sentinel.Attrs))
	for k := range sentinel.Attrs {
		toolNames = append(toolNames, k)
	}

	for _, toolName := range toolNames {
		entity, err := v.graphStore.GetEntity(ctx, "cooccurrence:tool:"+toolName)
		if err != nil {
			logr.Warn("failed to load co-occurrence entity", "tool", toolName, "error", err)
			continue
		}
		if entity == nil {
			continue
		}

		if v.cooccurrence[toolName] == nil {
			v.cooccurrence[toolName] = make(map[string]int)
		}

		for k, val := range entity.Attrs {
			if !strings.HasPrefix(k, cooccurrenceAttrPrefix) {
				continue
			}
			otherTool := strings.TrimPrefix(k, cooccurrenceAttrPrefix)
			count, err := strconv.Atoi(val)
			if err != nil {
				continue
			}
			v.cooccurrence[toolName][otherTool] = count
			if count > v.maxEdgeWeight {
				v.maxEdgeWeight = count
			}
		}
	}

	logr.Info("co-occurrence graph loaded from graph store", "tools", len(toolNames), "maxEdgeWeight", v.maxEdgeWeight)
	return nil
}

// persistCooccurrence writes the co-occurrence data for the given changed
// tools to the graph store. Each tool becomes an Entity with attrs encoding
// its pairwise counts. A sentinel "cooccurrence:index" entity tracks all
// known tool names for discovery during load.
func (v *VectorToolProvider) persistCooccurrence(ctx context.Context, changed map[string]struct{}) {
	logr := logger.GetLogger(ctx).With("fn", "VectorToolProvider.persistCooccurrence")

	v.mu.RLock()
	defer v.mu.RUnlock()

	for toolName := range changed {
		edges := v.cooccurrence[toolName]
		attrs := make(map[string]string, len(edges))
		for otherTool, count := range edges {
			attrs[cooccurrenceAttrPrefix+otherTool] = strconv.Itoa(count)
		}

		entity := graph.Entity{
			ID:    "cooccurrence:tool:" + toolName,
			Type:  cooccurrenceEntityType,
			Attrs: attrs,
		}
		if err := v.graphStore.AddEntity(ctx, entity); err != nil {
			logr.Warn("failed to persist co-occurrence entity", "tool", toolName, "error", err)
		}
	}

	// Update the sentinel index entity with all known tool names.
	allTools := make(map[string]string, len(v.cooccurrence))
	for toolName := range v.cooccurrence {
		allTools[toolName] = "1"
	}
	indexEntity := graph.Entity{
		ID:    "cooccurrence:index",
		Type:  cooccurrenceEntityType,
		Attrs: allTools,
	}
	if err := v.graphStore.AddEntity(ctx, indexEntity); err != nil {
		logr.Warn("failed to persist co-occurrence index", "error", err)
	}
}

// CooccurrenceScore returns a normalized affinity score [0, 1] between
// two tools based on how frequently they appear together. Returns 0 if
// either tool is unknown or the graph is empty.
//
// When DisableCooccurrenceCache is true and graphStore is available, the
// score is computed directly from the graph store entities.
func (v *VectorToolProvider) CooccurrenceScore(toolA, toolB string) float64 {
	if v.DisableCooccurrenceCache && v.graphStore != nil {
		return v.cooccurrenceScoreFromGraph(toolA, toolB)
	}

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

// cooccurrenceScoreFromGraph reads co-occurrence counts directly from the
// graph store, bypassing the in-memory cache. Used when DisableCooccurrenceCache is true.
func (v *VectorToolProvider) cooccurrenceScoreFromGraph(toolA, toolB string) float64 {
	ctx := context.Background()

	entityA, err := v.graphStore.GetEntity(ctx, "cooccurrence:tool:"+toolA)
	if err != nil || entityA == nil {
		return 0
	}

	val, ok := entityA.Attrs[cooccurrenceAttrPrefix+toolB]
	if !ok {
		return 0
	}
	weight, err := strconv.Atoi(val)
	if err != nil || weight == 0 {
		return 0
	}

	// Need maxEdgeWeight for normalization. Read from the cache (still
	// tracked even when cache is disabled) because scanning all entities
	// for the global max would be expensive.
	v.mu.RLock()
	max := v.maxEdgeWeight
	v.mu.RUnlock()

	if max == 0 {
		return 0
	}

	return math.Log1p(float64(weight)) / math.Log1p(float64(max))
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

// String formats search results as a concise string suitable for
// inclusion in LLM prompts. Each tool is listed as "- name: description".
func (results ToolSearchResults) String() string {
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
