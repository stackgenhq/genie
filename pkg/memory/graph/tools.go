package graph

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// Tool names for the consolidated graph tools. Instead of 5 separate tools
// (graph_store_entity, graph_store_relation, graph_query, graph_get_entity,
// graph_shortest_path), we expose 2 action-routed tools to reduce tool
// explosion while preserving functionality.
const (
	// GraphStoreToolName covers entity and relation storage.
	GraphStoreToolName = "graph_store"
	// GraphQueryToolName covers neighbor queries, entity lookups, and shortest path.
	GraphQueryToolName = "graph_query"
)

// ---- graph_store tool (entities + relations) ----

// GraphStoreRequest is the input for the unified graph_store tool.
// Set Action to "entity" or "relation" to choose what to store.
type GraphStoreRequest struct {
	Action string `json:"action" jsonschema:"description=What to store: 'entity' or 'relation',required,enum=entity,enum=relation"`

	// Fields for action=entity
	ID    string            `json:"id,omitempty" jsonschema:"description=Unique identifier for the entity (required when action=entity)"`
	Type  string            `json:"type,omitempty" jsonschema:"description=Entity type e.g. person repo issue document (required when action=entity)"`
	Attrs map[string]string `json:"attrs,omitempty" jsonschema:"description=Optional key-value attributes for the entity"`

	// Fields for action=relation
	SubjectID string `json:"subject_id,omitempty" jsonschema:"description=ID of the subject entity (required when action=relation)"`
	Predicate string `json:"predicate,omitempty" jsonschema:"description=Relation type e.g. WORKED_ON OWNS MENTIONS (required when action=relation)"`
	ObjectID  string `json:"object_id,omitempty" jsonschema:"description=ID of the object entity (required when action=relation)"`
}

// GraphStoreResponse is the output for the graph_store tool.
type GraphStoreResponse struct {
	Message string `json:"message"`
}

// newGraphStoreTool creates the unified graph_store tool. Unexported because
// it is only used by ToolProvider within this package.
func newGraphStoreTool(store IStore) tool.Tool {
	t := &graphStoreTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(GraphStoreToolName),
		function.WithDescription(
			"Store an entity or relation in the knowledge graph. "+
				"Use action='entity' with id, type, and optional attrs to store a node (person, repo, issue, document, etc.). "+
				"Use action='relation' with subject_id, predicate, object_id to store a directed edge (e.g. person-1 WORKED_ON issue-2). "+
				"Entities are overwritten if the same id is used. Relations are idempotent.",
		),
	)
}

type graphStoreTool struct {
	store IStore
}

// execute routes the graph_store request to AddEntity or AddRelation based on action.
func (t *graphStoreTool) execute(ctx context.Context, req GraphStoreRequest) (GraphStoreResponse, error) {
	switch req.Action {
	case "entity":
		if req.ID == "" || req.Type == "" {
			return GraphStoreResponse{}, fmt.Errorf("%w: id and type are required for action=entity", ErrInvalidInput)
		}
		if err := t.store.AddEntity(ctx, Entity{
			ID:    req.ID,
			Type:  req.Type,
			Attrs: req.Attrs,
		}); err != nil {
			return GraphStoreResponse{}, err
		}
		return GraphStoreResponse{Message: "Entity stored"}, nil

	case "relation":
		if req.SubjectID == "" || req.Predicate == "" || req.ObjectID == "" {
			return GraphStoreResponse{}, fmt.Errorf("%w: subject_id, predicate, and object_id are required for action=relation", ErrInvalidInput)
		}
		if err := t.store.AddRelation(ctx, Relation{
			SubjectID: req.SubjectID,
			Predicate: req.Predicate,
			ObjectID:  req.ObjectID,
		}); err != nil {
			return GraphStoreResponse{}, err
		}
		return GraphStoreResponse{Message: "Relation stored"}, nil

	default:
		return GraphStoreResponse{}, fmt.Errorf("%w: action must be 'entity' or 'relation', got %q", ErrInvalidInput, req.Action)
	}
}

// ---- graph_query tool (neighbors + get_entity + shortest_path) ----

// GraphQueryRequest is the input for the unified graph_query tool.
// Set Action to "neighbors", "get_entity", or "shortest_path".
type GraphQueryRequest struct {
	Action string `json:"action" jsonschema:"description=Query type: 'neighbors' or 'get_entity' or 'shortest_path',required,enum=neighbors,enum=get_entity,enum=shortest_path"`

	// Fields for action=neighbors and action=get_entity
	EntityID string `json:"entity_id,omitempty" jsonschema:"description=ID of the entity to query or look up (required for neighbors and get_entity)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max number of neighbors to return when action=neighbors (default 20)"`

	// Fields for action=shortest_path
	SourceID string `json:"source_id,omitempty" jsonschema:"description=ID of the starting entity (required when action=shortest_path)"`
	TargetID string `json:"target_id,omitempty" jsonschema:"description=ID of the destination entity (required when action=shortest_path)"`
}

// GraphQueryResponse is the output for the graph_query tool.
type GraphQueryResponse struct {
	// For action=neighbors
	Neighbors []Neighbor `json:"neighbors,omitempty"`
	Count     int        `json:"count,omitempty"`

	// For action=get_entity
	Entity *Entity `json:"entity,omitempty"`
	Found  bool    `json:"found"`

	// For action=shortest_path
	Path []string `json:"path,omitempty"`
}

// newGraphQueryTool creates the unified graph_query tool. Unexported because
// it is only used by ToolProvider within this package.
func newGraphQueryTool(store IStore) tool.Tool {
	t := &graphQueryTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(GraphQueryToolName),
		function.WithDescription(
			"Query the knowledge graph. "+
				"Use action='neighbors' with entity_id to get one-hop neighbors (entities linked by a relation). "+
				"Use action='get_entity' with entity_id to look up an entity by ID and get its type and attributes. "+
				"Use action='shortest_path' with source_id and target_id to find the shortest path between two entities.",
		),
	)
}

type graphQueryTool struct {
	store IStore
}

// execute routes the graph_query request to Neighbors, GetEntity, or ShortestPath.
func (t *graphQueryTool) execute(ctx context.Context, req GraphQueryRequest) (GraphQueryResponse, error) {
	switch req.Action {
	case "neighbors":
		if req.EntityID == "" {
			return GraphQueryResponse{}, fmt.Errorf("%w: entity_id is required for action=neighbors", ErrInvalidInput)
		}
		limit := req.Limit
		if limit <= 0 {
			limit = 20
		}
		neighbors, err := t.store.Neighbors(ctx, req.EntityID, limit)
		if err != nil {
			return GraphQueryResponse{}, err
		}
		return GraphQueryResponse{Neighbors: neighbors, Count: len(neighbors), Found: len(neighbors) > 0}, nil

	case "get_entity":
		if req.EntityID == "" {
			return GraphQueryResponse{}, fmt.Errorf("%w: entity_id is required for action=get_entity", ErrInvalidInput)
		}
		entity, err := t.store.GetEntity(ctx, req.EntityID)
		if err != nil {
			return GraphQueryResponse{}, err
		}
		if entity == nil {
			return GraphQueryResponse{Found: false}, nil
		}
		return GraphQueryResponse{Entity: entity, Found: true}, nil

	case "shortest_path":
		if req.SourceID == "" || req.TargetID == "" {
			return GraphQueryResponse{}, fmt.Errorf("%w: source_id and target_id are required for action=shortest_path", ErrInvalidInput)
		}
		path, err := t.store.ShortestPath(ctx, req.SourceID, req.TargetID)
		if err != nil {
			return GraphQueryResponse{}, err
		}
		return GraphQueryResponse{Path: path, Found: len(path) > 0}, nil

	default:
		return GraphQueryResponse{}, fmt.Errorf("%w: action must be 'neighbors', 'get_entity', or 'shortest_path', got %q", ErrInvalidInput, req.Action)
	}
}
