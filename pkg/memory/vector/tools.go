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
func NewMemoryStoreTool(store IStore, cfg *Config) tool.Tool {
	s := &memoryStoreTool{store: store, cfg: cfg}
	return function.NewFunctionTool(
		s.execute,
		function.WithName("memory_store"),
		function.WithDescription("Store a piece of text into long-term vector memory for later retrieval. Use this to remember important facts, decisions, or observations. Optional id enables upsert (replace existing memory with the same id)."),
	)
}

type memoryStoreTool struct {
	store IStore
	cfg   *Config
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
	item := BatchItem{ID: id, Text: redactedText, Metadata: redactedMeta}
	if req.ID != "" {
		if err := t.store.Upsert(ctx, item); err != nil {
			return MemoryStoreResponse{}, fmt.Errorf("failed to upsert memory: %w", err)
		}
		return MemoryStoreResponse{ID: id, Message: "Successfully upserted in memory"}, nil
	}
	if err := t.store.Add(ctx, item); err != nil {
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
		function.WithName("memory_search"),
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

	var results []SearchResult
	var err error
	if len(req.Filter) > 0 {
		results, err = t.store.SearchWithFilter(ctx, req.Query, limit, req.Filter)
	} else {
		results, err = t.store.Search(ctx, req.Query, limit)
	}
	if err != nil {
		return MemorySearchResponse{}, fmt.Errorf("memory search failed: %w", err)
	}

	items := make([]MemorySearchResultItem, 0, len(results))
	for _, r := range results {
		items = append(items, MemorySearchResultItem{
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
