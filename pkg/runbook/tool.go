package runbook

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/appcd-dev/genie/pkg/memory/vector"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// searchRunbookRequest is the input for the search_runbook tool.
type searchRunbookRequest struct {
	Query string `json:"query" jsonschema:"description=The search query to find relevant runbook content,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Maximum number of results to return (default 5)"`
}

// searchRunbookResultItem represents a single search result.
type searchRunbookResultItem struct {
	Source     string  `json:"source"`
	Content    string  `json:"content"`
	Similarity float64 `json:"similarity"`
}

// searchRunbookResponse is the output for the search_runbook tool.
type searchRunbookResponse struct {
	Results []searchRunbookResultItem `json:"results"`
	Count   int                       `json:"count"`
}

// MarshalJSON implements custom JSON marshaling for tool responses.
func (r searchRunbookResponse) MarshalJSON() ([]byte, error) {
	type alias searchRunbookResponse
	return json.Marshal(alias(r))
}

// NewSearchTool creates a tool that searches runbook content stored in
// the vector store. Runbook documents are indexed with metadata
// type=runbook and source=<filename>, enabling semantic search over
// customer-provided instructions, deployment guides, and playbooks.
func NewSearchTool(store vector.IStore) tool.Tool {
	t := &searchRunbookTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName("search_runbook"),
		function.WithDescription(
			"Search customer-provided runbooks for relevant instructions. "+
				"Runbooks contain deployment procedures, troubleshooting playbooks, "+
				"coding standards, and other operational guides. Use this tool when "+
				"you need to follow specific procedures or check for existing instructions "+
				"before taking action."),
	)
}

type searchRunbookTool struct {
	store vector.IStore
}

func (t *searchRunbookTool) execute(ctx context.Context, req searchRunbookRequest) (searchRunbookResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}

	results, err := t.store.Search(ctx, req.Query, limit)
	if err != nil {
		return searchRunbookResponse{}, fmt.Errorf("runbook search failed: %w", err)
	}

	// Filter to runbook-type results only.
	items := make([]searchRunbookResultItem, 0, len(results))
	for _, r := range results {
		if r.Metadata["type"] != "runbook" {
			continue
		}
		items = append(items, searchRunbookResultItem{
			Source:     r.Metadata["source"],
			Content:    r.Content,
			Similarity: r.Score,
		})
	}

	return searchRunbookResponse{
		Results: items,
		Count:   len(items),
	}, nil
}
