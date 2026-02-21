package vector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/appcd-dev/genie/pkg/pii"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ---- memory_store tool ----

// MemoryStoreRequest is the input for the memory_store tool.
type MemoryStoreRequest struct {
	Text     string            `json:"text" jsonschema:"description=The text content to store in memory,required"`
	Metadata map[string]string `json:"metadata,omitempty" jsonschema:"description=Optional key-value metadata to attach"`
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
// The tool generates a UUID for each stored document and returns it in the response.
func NewMemoryStoreTool(store IStore) tool.Tool {
	s := &memoryStoreTool{store: store}
	return function.NewFunctionTool(
		s.execute,
		function.WithName("memory_store"),
		function.WithDescription("Store a piece of text into long-term vector memory for later retrieval. Use this to remember important facts, decisions, or observations."),
	)
}

type memoryStoreTool struct {
	store IStore
}

func (t *memoryStoreTool) execute(ctx context.Context, req MemoryStoreRequest) (MemoryStoreResponse, error) {
	id := uuid.New().String()
	// PII-redact text before persisting to vector store.
	redactedText := pii.Redact(req.Text)
	redactedMeta := pii.RedactMap(req.Metadata)
	if err := t.store.Add(ctx, BatchItem{ID: id, Text: redactedText, Metadata: redactedMeta}); err != nil {
		return MemoryStoreResponse{}, fmt.Errorf("failed to store memory: %w", err)
	}
	return MemoryStoreResponse{
		ID:      id,
		Message: "Successfully stored in memory",
	}, nil
}

// ---- memory_search tool ----

// MemorySearchRequest is the input for the memory_search tool.
type MemorySearchRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to find relevant memories,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Maximum number of results to return (default 5)"`
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

// NewMemorySearchTool creates a tool that searches the vector memory.
// It embeds the query text and performs cosine similarity search across
// all stored documents, returning the most relevant results.
func NewMemorySearchTool(store IStore) tool.Tool {
	s := &memorySearchTool{store: store}
	return function.NewFunctionTool(
		s.execute,
		function.WithName("memory_search"),
		function.WithDescription("Search long-term vector memory for previously stored information. Returns the most semantically similar results."),
	)
}

type memorySearchTool struct {
	store IStore
}

func (t *memorySearchTool) execute(ctx context.Context, req MemorySearchRequest) (MemorySearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	results, err := t.store.Search(ctx, req.Query, limit)
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
