// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/pii"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// Tool name constants for the vector memory tools. Use these instead of
// magic strings when referencing memory tools elsewhere (e.g. retrieval
// tool classification, empty-memory guard, loop detection).
const (
	MemoryStoreToolName  = "memory_store"
	MemorySearchToolName = "memory_search"
	MemoryDeleteToolName = "memory_delete"
	MemoryListToolName   = "memory_list"
	MemoryMergeToolName  = "memory_merge"
)

// allowedMetadataKeys returns a set of allowed keys for validation. If cfg is nil
// or AllowedMetadataKeys is empty, nil is returned meaning "allow any key".
func allowedMetadataKeys(cfg *Config) map[string]bool {
	if cfg == nil || len(cfg.AllowedMetadataKeys) == 0 {
		return nil
	}
	m := make(map[string]bool, len(cfg.AllowedMetadataKeys))
	for _, k := range cfg.AllowedMetadataKeys {
		m[k] = true
	}
	return m
}

// validateMetadataKeys returns an error if any key in m is not in allowed. If
// allowed is nil, no validation is performed.
func validateMetadataKeys(m map[string]string, allowed map[string]bool) error {
	if allowed == nil {
		return nil
	}
	for k := range m {
		if !allowed[k] {
			return fmt.Errorf("metadata key %q is not allowed; allowed keys: %s", k, strings.Join(mapKeys(allowed), ", "))
		}
	}
	return nil
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MemoryImportanceScorer scores text content for importance on a 1-10 scale.
// This is a local interface to avoid importing reactree/memory; callers can
// satisfy it with rtmemory.ImportanceScorer or a no-op implementation.
//
//counterfeiter:generate . MemoryImportanceScorer
type MemoryImportanceScorer interface {
	// ScoreText returns an importance score (1-10) for the given text.
	ScoreText(ctx context.Context, text string) int
}

// ---- memory_store tool ----

// MemoryStoreRequest is the input for the memory_store tool.
type MemoryStoreRequest struct {
	Text     string            `json:"text" jsonschema:"description=The text content to store in memory,required"`
	Metadata map[string]string `json:"metadata,omitempty" jsonschema:"description=Optional key-value metadata to attach (keys must be in allowed_metadata_keys when configured)"`
	ID       string            `json:"id,omitempty" jsonschema:"description=Optional stable ID for upsert; if provided, any existing memory with this ID is replaced"`
}

// MemoryStoreResponse is the output for the memory_store tool.
type MemoryStoreResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// MarshalJSON implements custom JSON marshaling for tool responses.
func (r MemoryStoreResponse) MarshalJSON() ([]byte, error) {
	type alias MemoryStoreResponse
	return json.Marshal(alias(r))
}

// NewMemoryStoreTool creates a tool that stores text into the vector memory.
// When cfg.AllowedMetadataKeys is set, only those keys are accepted in metadata.
// If req.ID is set, the store is upserted (existing document with that ID is replaced).
// If scorer is non-nil, each write is scored for importance and the score is
// stored as "_importance" metadata for retrieval-time quality filtering.
func NewMemoryStoreTool(store IStore, cfg *Config, scorer MemoryImportanceScorer) tool.Tool {
	s := &memoryStoreTool{store: store, cfg: cfg, scorer: scorer}
	return function.NewFunctionTool(
		s.execute,
		function.WithName(MemoryStoreToolName),
		function.WithDescription("Store a piece of text into long-term vector memory for later retrieval. Use this to remember important facts, decisions, or observations. Optional id enables upsert (replace existing memory with the same id)."),
	)
}

type memoryStoreTool struct {
	store  IStore
	cfg    *Config
	scorer MemoryImportanceScorer
}

func (t *memoryStoreTool) execute(ctx context.Context, req MemoryStoreRequest) (MemoryStoreResponse, error) {
	allowed := allowedMetadataKeys(t.cfg)
	if err := validateMetadataKeys(req.Metadata, allowed); err != nil {
		return MemoryStoreResponse{}, fmt.Errorf("invalid metadata: %w", err)
	}
	id := req.ID
	if id == "" {
		id = uuid.New().String()
	}
	// PII-redact text before persisting to vector store.
	redactedText := pii.Redact(req.Text)
	redactedMeta := pii.RedactMap(req.Metadata)

	// Enrich metadata with importance score when a scorer is available.
	if t.scorer != nil {
		score := t.scorer.ScoreText(ctx, redactedText)
		if redactedMeta == nil {
			redactedMeta = make(map[string]string)
		}
		redactedMeta["_importance"] = fmt.Sprintf("%d", score)
	}

	item := BatchItem{ID: id, Text: redactedText, Metadata: redactedMeta}
	if req.ID != "" {
		if err := t.store.Upsert(ctx, UpsertRequest{Items: []BatchItem{item}}); err != nil {
			return MemoryStoreResponse{}, fmt.Errorf("failed to upsert memory: %w", err)
		}
		return MemoryStoreResponse{ID: id, Message: "Successfully upserted in memory"}, nil
	}
	if err := t.store.Add(ctx, AddRequest{Items: []BatchItem{item}}); err != nil {
		return MemoryStoreResponse{}, fmt.Errorf("failed to store memory: %w", err)
	}
	return MemoryStoreResponse{ID: id, Message: "Successfully stored in memory"}, nil
}

// ---- memory_search tool ----

// MemorySearchRequest is the input for the memory_search tool.
type MemorySearchRequest struct {
	Query  string            `json:"query" jsonschema:"description=The search query to find relevant memories,required"`
	Limit  int               `json:"limit,omitempty" jsonschema:"description=Maximum number of results to return (default 5)"`
	Filter map[string]string `json:"filter,omitempty" jsonschema:"description=Optional metadata filter (e.g. product, category); keys must be in allowed_metadata_keys when configured"`
}

// MemorySearchResultItem represents a single search result from the memory_search tool.
type MemorySearchResultItem struct {
	ID         string            `json:"id"`
	Content    string            `json:"content"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Similarity float64           `json:"similarity"`
}

// MemorySearchResponse is the output for the memory_search tool.
type MemorySearchResponse struct {
	Results []MemorySearchResultItem `json:"results"`
	Count   int                      `json:"count"`
}

// MarshalJSON implements custom JSON marshaling for tool responses.
func (r MemorySearchResponse) MarshalJSON() ([]byte, error) {
	type alias MemorySearchResponse
	return json.Marshal(alias(r))
}

// memorySearchDescription is the tool description shown to the LLM. It steers the
// agent to prefer memory_search over per-source tools (Gmail, Drive, Slack, etc.)
// when the user wants to find or search information, following the pattern used
// elsewhere: semantic search over pre-indexed content first; direct API tools for
// live data or actions (see e.g. TURA/agentic RAG guidance).
const memorySearchDescription = "Search the unified long-term memory index. " +
	"When unified data sources are enabled, this index includes synced content from Gmail, Google Drive, Slack, Linear, GitHub, and Calendar. " +
	"USE THIS FIRST when the user wants to find, search, or recall information that could be in email, documents, chat messages, or issues — one semantic search returns relevant results across all connected sources. " +
	"Results include source metadata (e.g. source, source_ref_id) so you can open or fetch the original item with other tools if needed. " +
	"Use source-specific tools (e.g. gmail_list_messages, google_drive_read_file) only when you need the very latest content, to open a specific item by ID, or to perform an action (send, create, update). " +
	"Optional filter restricts by metadata (e.g. product, category, source); keys must be in allowed_metadata_keys when configured."

// NewMemorySearchTool creates a tool that searches the vector memory.
// When cfg.AllowedMetadataKeys is set, only those keys may be used in filter.
func NewMemorySearchTool(store IStore, cfg *Config) tool.Tool {
	s := &memorySearchTool{store: store, cfg: cfg}
	return function.NewFunctionTool(
		s.execute,
		function.WithName(MemorySearchToolName),
		function.WithDescription(memorySearchDescription),
	)
}

type memorySearchTool struct {
	store IStore
	cfg   *Config
}

func (t *memorySearchTool) execute(ctx context.Context, req MemorySearchRequest) (MemorySearchResponse, error) {
	allowed := allowedMetadataKeys(t.cfg)
	if err := validateMetadataKeys(req.Filter, allowed); err != nil {
		return MemorySearchResponse{}, fmt.Errorf("invalid filter: %w", err)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	results, err := t.store.Search(ctx, SearchRequest{
		Query:  req.Query,
		Limit:  limit,
		Filter: req.Filter,
	})
	if err != nil {
		return MemorySearchResponse{}, fmt.Errorf("memory search failed: %w", err)
	}

	items := make([]MemorySearchResultItem, 0, len(results))
	for _, r := range results {
		items = append(items, MemorySearchResultItem{
			ID:         r.ID,
			Content:    r.Content,
			Metadata:   r.Metadata,
			Similarity: r.Score,
		})
	}

	return MemorySearchResponse{
		Results: items,
		Count:   len(items),
	}, nil
}

// ---- memory_delete tool ----

// MemoryDeleteRequest is the input for the memory_delete tool.
type MemoryDeleteRequest struct {
	IDs []string `json:"ids" jsonschema:"description=List of memory IDs to delete,required"`
}

// MemoryDeleteResponse is the output for the memory_delete tool.
type MemoryDeleteResponse struct {
	Deleted int    `json:"deleted"`
	Message string `json:"message"`
}

// MarshalJSON implements custom JSON marshaling for tool responses.
func (r MemoryDeleteResponse) MarshalJSON() ([]byte, error) {
	type alias MemoryDeleteResponse
	return json.Marshal(alias(r))
}

// NewMemoryDeleteTool creates a tool that deletes entries from vector memory by ID.
// Use this to clean up stale, incorrect, or outdated memories.
func NewMemoryDeleteTool(store IStore) tool.Tool {
	s := &memoryDeleteTool{store: store}
	return function.NewFunctionTool(
		s.execute,
		function.WithName(MemoryDeleteToolName),
		function.WithDescription(
			"Delete one or more entries from long-term vector memory by their IDs. "+
				"Use this to clean up stale, incorrect, or outdated memories that are "+
				"polluting search results. First use memory_search to find the IDs of "+
				"entries you want to remove, then call this tool with those IDs."),
	)
}

type memoryDeleteTool struct {
	store IStore
}

func (t *memoryDeleteTool) execute(ctx context.Context, req MemoryDeleteRequest) (MemoryDeleteResponse, error) {
	if len(req.IDs) == 0 {
		return MemoryDeleteResponse{}, fmt.Errorf("at least one ID is required")
	}
	if err := t.store.Delete(ctx, DeleteRequest{IDs: req.IDs}); err != nil {
		return MemoryDeleteResponse{}, fmt.Errorf("failed to delete memories: %w", err)
	}
	return MemoryDeleteResponse{
		Deleted: len(req.IDs),
		Message: fmt.Sprintf("Successfully deleted %d memory entries", len(req.IDs)),
	}, nil
}

// ---- memory_list tool ----

// MemoryListRequest is the input for the memory_list tool.
type MemoryListRequest struct {
	Filter map[string]string `json:"filter,omitempty" jsonschema:"description=Optional metadata filter to narrow results (e.g. type=accomplishment)"`
	Limit  int               `json:"limit,omitempty" jsonschema:"description=Maximum entries to return (default 20)"`
}

// MemoryListResponse is the output for the memory_list tool.
type MemoryListResponse struct {
	Entries []MemorySearchResultItem `json:"entries"`
	Count   int                      `json:"count"`
}

// MarshalJSON implements custom JSON marshaling for tool responses.
func (r MemoryListResponse) MarshalJSON() ([]byte, error) {
	type alias MemoryListResponse
	return json.Marshal(alias(r))
}

// NewMemoryListTool creates a tool that lists entries from vector memory.
// Supports metadata filtering to browse specific categories.
func NewMemoryListTool(store IStore, cfg *Config) tool.Tool {
	s := &memoryListTool{store: store, cfg: cfg}
	return function.NewFunctionTool(
		s.execute,
		function.WithName(MemoryListToolName),
		function.WithDescription(
			"List entries stored in long-term vector memory. "+
				"Use optional filter to narrow by metadata (e.g. {\"type\": \"accomplishment\"}). "+
				"Returns entries with their IDs, content, and metadata. "+
				"Use this to audit what the agent has remembered and identify stale entries for deletion."),
	)
}

type memoryListTool struct {
	store IStore
	cfg   *Config
}

func (t *memoryListTool) execute(ctx context.Context, req MemoryListRequest) (MemoryListResponse, error) {
	allowed := allowedMetadataKeys(t.cfg)
	if err := validateMetadataKeys(req.Filter, allowed); err != nil {
		return MemoryListResponse{}, fmt.Errorf("invalid filter: %w", err)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}

	var results []SearchResult
	var err error
	if len(req.Filter) > 0 {
		results, err = t.store.Search(ctx, SearchRequest{Query: "", Limit: limit, Filter: req.Filter})
	} else {
		// Without a filter or query, use a broad search term.
		results, err = t.store.Search(ctx, SearchRequest{Query: "memory", Limit: limit})
	}
	if err != nil {
		return MemoryListResponse{}, fmt.Errorf("memory list failed: %w", err)
	}

	items := make([]MemorySearchResultItem, 0, len(results))
	for _, r := range results {
		items = append(items, MemorySearchResultItem{
			ID:         r.ID,
			Content:    r.Content,
			Metadata:   r.Metadata,
			Similarity: r.Score,
		})
	}

	return MemoryListResponse{
		Entries: items,
		Count:   len(items),
	}, nil
}

// ---- memory_merge tool ----

// MemoryMergeRequest is the input for the memory_merge tool.
type MemoryMergeRequest struct {
	IDs        []string          `json:"ids" jsonschema:"description=List of memory IDs to merge (minimum 2),required"`
	MergedText string            `json:"merged_text" jsonschema:"description=The consolidated text for the merged memory entry,required"`
	Metadata   map[string]string `json:"metadata,omitempty" jsonschema:"description=Optional metadata for the merged entry; if omitted metadata from the first ID is used"`
}

// MemoryMergeResponse is the output for the memory_merge tool.
type MemoryMergeResponse struct {
	MergedID     string `json:"merged_id"`
	DeletedCount int    `json:"deleted_count"`
	Message      string `json:"message"`
}

// MarshalJSON implements custom JSON marshaling for tool responses.
func (r MemoryMergeResponse) MarshalJSON() ([]byte, error) {
	type alias MemoryMergeResponse
	return json.Marshal(alias(r))
}

// NewMemoryMergeTool creates a tool that merges multiple memory entries into one.
// The agent provides the consolidated text; the tool upserts it under the first ID
// and deletes the remaining IDs.
func NewMemoryMergeTool(store IStore, cfg *Config) tool.Tool {
	s := &memoryMergeTool{store: store, cfg: cfg}
	return function.NewFunctionTool(
		s.execute,
		function.WithName(MemoryMergeToolName),
		function.WithDescription(
			"Merge multiple memory entries into a single consolidated entry. "+
				"First use memory_search or memory_list to find related/duplicate entries, "+
				"then provide their IDs and the merged text. The merged entry is saved under "+
				"the first ID; remaining entries are deleted. Use this to consolidate "+
				"fragmented or duplicated memories."),
	)
}

type memoryMergeTool struct {
	store IStore
	cfg   *Config
}

func (t *memoryMergeTool) execute(ctx context.Context, req MemoryMergeRequest) (MemoryMergeResponse, error) {
	if len(req.IDs) < 2 {
		return MemoryMergeResponse{}, fmt.Errorf("at least 2 IDs are required to merge")
	}
	if strings.TrimSpace(req.MergedText) == "" {
		return MemoryMergeResponse{}, fmt.Errorf("merged_text cannot be empty")
	}
	allowed := allowedMetadataKeys(t.cfg)
	if err := validateMetadataKeys(req.Metadata, allowed); err != nil {
		return MemoryMergeResponse{}, fmt.Errorf("invalid metadata: %w", err)
	}

	// PII-redact the merged text before persisting.
	redactedText := pii.Redact(req.MergedText)
	redactedMeta := pii.RedactMap(req.Metadata)

	// Upsert the merged entry under the first ID.
	mergedID := req.IDs[0]
	if err := t.store.Upsert(ctx, UpsertRequest{Items: []BatchItem{{
		ID:       mergedID,
		Text:     redactedText,
		Metadata: redactedMeta,
	}}}); err != nil {
		return MemoryMergeResponse{}, fmt.Errorf("failed to upsert merged memory: %w", err)
	}

	// Delete the remaining IDs.
	remaining := req.IDs[1:]
	if err := t.store.Delete(ctx, DeleteRequest{IDs: remaining}); err != nil {
		return MemoryMergeResponse{}, fmt.Errorf("merged entry saved but failed to delete originals: %w", err)
	}

	return MemoryMergeResponse{
		MergedID:     mergedID,
		DeletedCount: len(remaining),
		Message:      fmt.Sprintf("Successfully merged %d entries into %s", len(req.IDs), mergedID),
	}, nil
}
