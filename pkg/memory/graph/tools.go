package graph

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	StoreEntityToolName   = "graph_store_entity"
	StoreRelationToolName = "graph_store_relation"
	GraphQueryToolName    = "graph_query"
	GetEntityToolName     = "graph_get_entity"
	ShortestPathToolName  = "graph_shortest_path"
)

// ---- graph_store_entity tool ----

// StoreEntityRequest is the input for the graph_store_entity tool.
type StoreEntityRequest struct {
	ID    string            `json:"id" jsonschema:"description=Unique identifier for the entity,required"`
	Type  string            `json:"type" jsonschema:"description=Entity type (e.g. person, repo, issue, document),required"`
	Attrs map[string]string `json:"attrs,omitempty" jsonschema:"description=Optional key-value attributes"`
}

// StoreEntityResponse is the output for the graph_store_entity tool.
type StoreEntityResponse struct {
	Message string `json:"message"`
}

// newStoreEntityTool creates a tool that stores an entity in the graph. Unexported
// because it is only used by ToolProvider within this package.
func newStoreEntityTool(store IStore) tool.Tool {
	t := &storeEntityTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(StoreEntityToolName),
		function.WithDescription("Store an entity (node) in the knowledge graph. Use for people, repos, issues, documents, etc. Overwrites if id already exists."),
	)
}

type storeEntityTool struct {
	store IStore
}

func (t *storeEntityTool) execute(ctx context.Context, req StoreEntityRequest) (StoreEntityResponse, error) {
	if req.ID == "" || req.Type == "" {
		return StoreEntityResponse{}, ErrInvalidInput
	}
	// Explicit construction per PR review; struct conversion would work but reviewer requested this.
	if err := t.store.AddEntity(ctx, Entity{ //nolint:staticcheck // S1016 prefer explicit per review
		ID:    req.ID,
		Type:  req.Type,
		Attrs: req.Attrs,
	}); err != nil {
		return StoreEntityResponse{}, err
	}
	return StoreEntityResponse{Message: "Entity stored"}, nil
}

// ---- graph_store_relation tool ----

// StoreRelationRequest is the input for the graph_store_relation tool.
type StoreRelationRequest struct {
	SubjectID string `json:"subject_id" jsonschema:"description=ID of the subject entity,required"`
	Predicate string `json:"predicate" jsonschema:"description=Relation type (e.g. WORKED_ON, OWNS, MENTIONS),required"`
	ObjectID  string `json:"object_id" jsonschema:"description=ID of the object entity,required"`
}

// StoreRelationResponse is the output for the graph_store_relation tool.
type StoreRelationResponse struct {
	Message string `json:"message"`
}

// newStoreRelationTool creates a tool that stores a relation in the graph. Unexported
// because it is only used by ToolProvider within this package.
func newStoreRelationTool(store IStore) tool.Tool {
	t := &storeRelationTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(StoreRelationToolName),
		function.WithDescription("Store a directed relation between two entities (subject --[predicate]--> object). Example: person-1 WORKED_ON issue-2."),
	)
}

type storeRelationTool struct {
	store IStore
}

func (t *storeRelationTool) execute(ctx context.Context, req StoreRelationRequest) (StoreRelationResponse, error) {
	if req.SubjectID == "" || req.Predicate == "" || req.ObjectID == "" {
		return StoreRelationResponse{}, ErrInvalidInput
	}
	// Explicit construction per PR review; struct conversion would work but reviewer requested this.
	if err := t.store.AddRelation(ctx, Relation{ //nolint:staticcheck // S1016 prefer explicit per review
		SubjectID: req.SubjectID,
		Predicate: req.Predicate,
		ObjectID:  req.ObjectID,
	}); err != nil {
		return StoreRelationResponse{}, err
	}
	return StoreRelationResponse{Message: "Relation stored"}, nil
}

// ---- graph_query tool ----

// GraphQueryRequest is the input for the graph_query tool.
type GraphQueryRequest struct {
	EntityID string `json:"entity_id" jsonschema:"description=ID of the entity to query from,required"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max number of neighbors to return (default 20)"`
}

// GraphQueryResponse is the output for the graph_query tool.
type GraphQueryResponse struct {
	Neighbors []Neighbor `json:"neighbors"`
	Count     int        `json:"count"`
}

// newGraphQueryTool creates a tool that queries the graph for neighbors of an entity. Unexported
// because it is only used by ToolProvider within this package.
func newGraphQueryTool(store IStore) tool.Tool {
	t := &graphQueryTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(GraphQueryToolName),
		function.WithDescription("Get one-hop neighbors of an entity in the knowledge graph (entities linked by a relation). Use to follow relationships (e.g. who worked on what, which repo an issue belongs to)."),
	)
}

type graphQueryTool struct {
	store IStore
}

func (t *graphQueryTool) execute(ctx context.Context, req GraphQueryRequest) (GraphQueryResponse, error) {
	if req.EntityID == "" {
		return GraphQueryResponse{}, ErrInvalidInput
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	neighbors, err := t.store.Neighbors(ctx, req.EntityID, limit)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	return GraphQueryResponse{Neighbors: neighbors, Count: len(neighbors)}, nil
}

// ---- graph_get_entity tool ----

// GetEntityRequest is the input for the graph_get_entity tool.
type GetEntityRequest struct {
	EntityID string `json:"entity_id" jsonschema:"description=ID of the entity to look up,required"`
}

// GetEntityResponse is the output for the graph_get_entity tool.
type GetEntityResponse struct {
	Entity *Entity `json:"entity,omitempty" jsonschema:"description=The entity if found"`
	Found  bool    `json:"found" jsonschema:"description=Whether the entity exists"`
}

// newGetEntityTool creates a tool that looks up an entity by ID. Unexported
// because it is only used by ToolProvider within this package.
func newGetEntityTool(store IStore) tool.Tool {
	t := &getEntityTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(GetEntityToolName),
		function.WithDescription("Look up an entity in the knowledge graph by ID. Returns the entity's type and attributes if it exists. Use to get details of a specific person, repo, issue, or document."),
	)
}

type getEntityTool struct {
	store IStore
}

func (t *getEntityTool) execute(ctx context.Context, req GetEntityRequest) (GetEntityResponse, error) {
	if req.EntityID == "" {
		return GetEntityResponse{}, ErrInvalidInput
	}
	entity, err := t.store.GetEntity(ctx, req.EntityID)
	if err != nil {
		return GetEntityResponse{}, err
	}
	if entity == nil {
		return GetEntityResponse{Found: false}, nil
	}
	return GetEntityResponse{Entity: entity, Found: true}, nil
}

// ---- graph_shortest_path tool ----

// ShortestPathRequest is the input for the graph_shortest_path tool.
type ShortestPathRequest struct {
	SourceID string `json:"source_id" jsonschema:"description=ID of the starting entity,required"`
	TargetID string `json:"target_id" jsonschema:"description=ID of the destination entity,required"`
}

// ShortestPathResponse is the output for the graph_shortest_path tool.
type ShortestPathResponse struct {
	Path  []string `json:"path" jsonschema:"description=Ordered list of entity IDs from source to target, empty if no path"`
	Found bool     `json:"found" jsonschema:"description=Whether a path exists"`
}

// newShortestPathTool creates a tool that finds the shortest path between two entities. Unexported
// because it is only used by ToolProvider within this package.
func newShortestPathTool(store IStore) tool.Tool {
	t := &shortestPathTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(ShortestPathToolName),
		function.WithDescription("Find the shortest path of entities between a source and target in the knowledge graph. Use to discover how two things are connected (e.g. person to issue via repo, or document to project). Returns an ordered list of entity IDs."),
	)
}

type shortestPathTool struct {
	store IStore
}

func (t *shortestPathTool) execute(ctx context.Context, req ShortestPathRequest) (ShortestPathResponse, error) {
	if req.SourceID == "" || req.TargetID == "" {
		return ShortestPathResponse{}, ErrInvalidInput
	}
	path, err := t.store.ShortestPath(ctx, req.SourceID, req.TargetID)
	if err != nil {
		return ShortestPathResponse{}, err
	}
	return ShortestPathResponse{Path: path, Found: len(path) > 0}, nil
}
