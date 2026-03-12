// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
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

// batchEntityStore is an optional interface for stores that support
// batched entity insertion with parallel embedding generation.
// VectorBackedStore implements this; InMemoryStore does not.
type batchEntityStore interface {
	AddEntities(ctx context.Context, entities []Entity) error
}

// GraphStoreRequest is the input for the unified graph_store tool.
// Set Action to "entity", "relation", or "batch" to choose what to store.
type GraphStoreRequest struct {
	Action string `json:"action" jsonschema:"description=What to do: 'entity' (store entity)  'relation' (store relation)  'delete_entity' (delete entity and its relations)  'delete_relation' (delete a specific relation)  'delete_all' (remove ALL graph data)  'batch' (multiple items at once),required,enum=entity,enum=relation,enum=delete_entity,enum=delete_relation,enum=delete_all,enum=batch"`

	// Fields for action=entity
	ID    string            `json:"id,omitempty" jsonschema:"description=Unique identifier for the entity (required when action=entity)"`
	Type  string            `json:"type,omitempty" jsonschema:"description=Entity type e.g. person repo issue document (required when action=entity)"`
	Attrs map[string]string `json:"attrs,omitempty" jsonschema:"description=Optional key-value attributes for the entity"`

	// Fields for action=relation
	SubjectID string `json:"subject_id,omitempty" jsonschema:"description=ID of the subject entity (required when action=relation)"`
	Predicate string `json:"predicate,omitempty" jsonschema:"description=Relation type e.g. WORKED_ON OWNS MENTIONS (required when action=relation)"`
	ObjectID  string `json:"object_id,omitempty" jsonschema:"description=ID of the object entity (required when action=relation)"`

	// Fields for action=batch
	Items []BatchStoreItem `json:"items,omitempty" jsonschema:"description=Array of entities and/or relations to store in one call (required for action=batch and max 20). EFFICIENCY: use batch instead of multiple separate graph_store calls."`
}

// BatchStoreItem represents a single entity or relation within a batch store request.
type BatchStoreItem struct {
	Action string `json:"action" jsonschema:"description=What to do: 'entity' 'relation' 'delete_entity' or 'delete_relation'. Note: 'batch' cannot be nested.,required,enum=entity,enum=relation,enum=delete_entity,enum=delete_relation"`

	// Fields for action=entity
	ID    string            `json:"id,omitempty" jsonschema:"description=Entity ID (required when action=entity)"`
	Type  string            `json:"type,omitempty" jsonschema:"description=Entity type (required when action=entity)"`
	Attrs map[string]string `json:"attrs,omitempty" jsonschema:"description=Optional key-value attributes"`

	// Fields for action=relation
	SubjectID string `json:"subject_id,omitempty" jsonschema:"description=Subject entity ID (required when action=relation)"`
	Predicate string `json:"predicate,omitempty" jsonschema:"description=Relation type (required when action=relation)"`
	ObjectID  string `json:"object_id,omitempty" jsonschema:"description=Object entity ID (required when action=relation)"`
}

// BatchStoreResult contains the result (or error) for one item within a batch.
type BatchStoreResult struct {
	Index   int    `json:"index"`
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// GraphStoreResponse is the output for the graph_store tool.
type GraphStoreResponse struct {
	Message      string             `json:"message"`
	BatchResults []BatchStoreResult `json:"batch_results,omitempty"`
}

// maxBatchStoreSize is the maximum number of items in a batch store request.
const maxBatchStoreSize = 20

// newGraphStoreTool creates the unified graph_store tool. Unexported because
// it is only used by ToolProvider within this package.
func newGraphStoreTool(store IStore) tool.Tool {
	t := &graphStoreTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(GraphStoreToolName),
		function.WithDescription(
			"Store, update, or delete entities and relations in the knowledge graph. "+
				"Use action='entity' with id, type, and optional attrs to store a node. "+
				"Use action='relation' with subject_id, predicate, object_id to store a directed edge. "+
				"Use action='delete_entity' with id to delete an entity and all its incident relations. "+
				"Use action='delete_relation' with subject_id, predicate, object_id to delete a specific relation. "+
				"**Use action='batch'** with items array to process MULTIPLE items in ONE call — "+
				"this is much more efficient than making separate calls. "+
				"Example: {\"action\":\"batch\",\"items\":[{\"action\":\"entity\",\"id\":\"alice\",\"type\":\"person\"},{\"action\":\"relation\",\"subject_id\":\"alice\",\"predicate\":\"WORKS_ON\",\"object_id\":\"project-1\"}]}. "+
				"Entities are overwritten if the same id is used. Relations and deletes are idempotent. "+
				"EFFICIENCY: batch > multiple separate calls.",
		),
	)
}

type graphStoreTool struct {
	store IStore
}

// indexedEntity pairs an entity with its original position in a batch.
type indexedEntity struct {
	index  int
	entity Entity
}

// execute routes the graph_store request to AddEntity, AddRelation, or batch based on action.
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

	case "delete_entity":
		if req.ID == "" {
			return GraphStoreResponse{}, fmt.Errorf("%w: id is required for action=delete_entity", ErrInvalidInput)
		}
		if err := t.store.DeleteEntity(ctx, req.ID); err != nil {
			return GraphStoreResponse{}, err
		}
		return GraphStoreResponse{Message: "Entity deleted"}, nil

	case "delete_relation":
		if req.SubjectID == "" || req.Predicate == "" || req.ObjectID == "" {
			return GraphStoreResponse{}, fmt.Errorf("%w: subject_id, predicate, and object_id are required for action=delete_relation", ErrInvalidInput)
		}
		if err := t.store.DeleteRelation(ctx, Relation{
			SubjectID: req.SubjectID,
			Predicate: req.Predicate,
			ObjectID:  req.ObjectID,
		}); err != nil {
			return GraphStoreResponse{}, err
		}
		return GraphStoreResponse{Message: "Relation deleted"}, nil

	case "delete_all":
		if err := t.store.DeleteAll(ctx); err != nil {
			return GraphStoreResponse{}, err
		}
		return GraphStoreResponse{Message: "All graph data deleted"}, nil

	case "batch":
		return t.executeBatch(ctx, req.Items)

	default:
		return GraphStoreResponse{}, fmt.Errorf("%w: action must be 'entity', 'relation', 'delete_entity', 'delete_relation', 'delete_all', or 'batch', got %q", ErrInvalidInput, req.Action)
	}
}

// executeBatch stores multiple entities and relations in a single call.
// Entities are stored first (they may be referenced by relations). When the
// underlying store supports batched entity insertion (VectorBackedStore),
// entities are embedded concurrently for lower latency. Relations are then
// stored concurrently via errgroup. Individual item errors are captured
// per-result rather than aborting the entire batch.
func (t *graphStoreTool) executeBatch(ctx context.Context, items []BatchStoreItem) (GraphStoreResponse, error) {
	if len(items) == 0 {
		return GraphStoreResponse{}, fmt.Errorf(
			"%w: items array is required and must not be empty for action=batch",
			ErrInvalidInput,
		)
	}
	if len(items) > maxBatchStoreSize {
		return GraphStoreResponse{}, fmt.Errorf(
			"%w: batch size %d exceeds maximum of %d",
			ErrInvalidInput, len(items), maxBatchStoreSize,
		)
	}

	results := make([]BatchStoreResult, len(items))

	// Partition items into entities and relations (with original indices).
	var entities []indexedEntity
	var relationIndices []int

	for i, item := range items {
		switch item.Action {
		case "entity":
			if item.ID == "" || item.Type == "" {
				results[i] = BatchStoreResult{Index: i, Action: "entity", Error: "id and type are required"}
				continue
			}
			entities = append(entities, indexedEntity{
				index:  i,
				entity: Entity{ID: item.ID, Type: item.Type, Attrs: item.Attrs},
			})
		case "relation":
			relationIndices = append(relationIndices, i)
		case "delete_entity":
			if item.ID == "" {
				results[i] = BatchStoreResult{Index: i, Action: "delete_entity", Error: "id is required"}
				continue
			}
			br := BatchStoreResult{Index: i, Action: "delete_entity"}
			if err := t.store.DeleteEntity(ctx, item.ID); err != nil {
				br.Error = err.Error()
			} else {
				br.Message = "Entity deleted"
			}
			results[i] = br
		case "delete_relation":
			if item.SubjectID == "" || item.Predicate == "" || item.ObjectID == "" {
				results[i] = BatchStoreResult{Index: i, Action: "delete_relation", Error: "subject_id, predicate, and object_id are required"}
				continue
			}
			br := BatchStoreResult{Index: i, Action: "delete_relation"}
			if err := t.store.DeleteRelation(ctx, Relation{
				SubjectID: item.SubjectID,
				Predicate: item.Predicate,
				ObjectID:  item.ObjectID,
			}); err != nil {
				br.Error = err.Error()
			} else {
				br.Message = "Relation deleted"
			}
			results[i] = br
		default:
			results[i] = BatchStoreResult{Index: i, Action: item.Action, Error: fmt.Sprintf("unsupported batch sub-action %q", item.Action)}
		}
	}

	// Store entities — try the optimised batch path first.
	if len(entities) > 0 {
		t.storeEntitiesBatch(ctx, entities, results)
	}

	// Store relations concurrently via errgroup (entities are already persisted).
	if len(relationIndices) > 0 {
		var wg sync.WaitGroup
		for _, idx := range relationIndices {
			item := items[idx]
			if item.SubjectID == "" || item.Predicate == "" || item.ObjectID == "" {
				results[idx] = BatchStoreResult{Index: idx, Action: "relation", Error: "subject_id, predicate, and object_id are required"}
				continue
			}
			wg.Add(1)
			go func(i int, r Relation) {
				defer wg.Done()
				br := BatchStoreResult{Index: i, Action: "relation"}
				if err := t.store.AddRelation(ctx, r); err != nil {
					br.Error = err.Error()
				} else {
					br.Message = "Relation stored"
				}
				results[i] = br
			}(idx, Relation{SubjectID: item.SubjectID, Predicate: item.Predicate, ObjectID: item.ObjectID})
		}
		wg.Wait()
	}

	// Count successes for the summary message.
	ok := 0
	for _, r := range results {
		if r.Error == "" && r.Message != "" {
			ok++
		}
	}

	return GraphStoreResponse{
		Message:      fmt.Sprintf("%d/%d items stored successfully", ok, len(items)),
		BatchResults: results,
	}, nil
}

// storeEntitiesBatch stores a slice of entities. If the underlying store
// implements batchEntityStore (e.g. VectorBackedStore), all entities are
// stored in a single call with parallel embedding generation. Otherwise
// entities are stored individually via errgroup.
func (t *graphStoreTool) storeEntitiesBatch(ctx context.Context, entities []indexedEntity, results []BatchStoreResult) {
	// Fast path: VectorBackedStore.AddEntities (single Upsert, parallel embeddings).
	if bs, ok := t.store.(batchEntityStore); ok && len(entities) > 1 {
		plain := make([]Entity, len(entities))
		for i, ie := range entities {
			plain[i] = ie.entity
		}
		err := bs.AddEntities(ctx, plain)
		for _, ie := range entities {
			br := BatchStoreResult{Index: ie.index, Action: "entity"}
			if err != nil {
				br.Error = err.Error()
			} else {
				br.Message = "Entity stored"
			}
			results[ie.index] = br
		}
		return
	}

	// Fallback: store entities concurrently via errgroup.
	g, gCtx := errgroup.WithContext(ctx)
	for _, ie := range entities {
		ie := ie // capture
		g.Go(func() error {
			br := BatchStoreResult{Index: ie.index, Action: "entity"}
			if err := t.store.AddEntity(gCtx, ie.entity); err != nil {
				br.Error = err.Error()
			} else {
				br.Message = "Entity stored"
			}
			results[ie.index] = br
			return nil // capture error in result, don't abort
		})
	}
	_ = g.Wait()
}

// ---- graph_query tool (neighbors + get_entity + shortest_path + explore + batch) ----

// GraphQueryRequest is the input for the unified graph_query tool.
// Set Action to "neighbors", "get_entity", "shortest_path", "explore", or "batch".
type GraphQueryRequest struct {
	Action string `json:"action" jsonschema:"description=Query action. RECOMMENDED: use 'explore' first — it returns the entity plus all neighbors and relations in ONE call (replaces separate get_entity + neighbors calls). Use 'batch' to run multiple queries at once (e.g. explore 3 entities in 1 call). Fallbacks: 'get_entity' for single entity lookup and 'neighbors' for just neighbor list and 'shortest_path' for path finding.,required,enum=neighbors,enum=get_entity,enum=shortest_path,enum=explore,enum=batch"`

	// Fields for action=neighbors, get_entity, and explore
	EntityID string `json:"entity_id,omitempty" jsonschema:"description=ID of the entity to query (required for explore and neighbors and get_entity). Example: 'appcd-dev' or 'repo-123'"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max neighbors to return (default 20). Applies to action=neighbors and action=explore"`

	// Fields for action=explore
	Depth int `json:"depth,omitempty" jsonschema:"description=How many hops to traverse for action=explore. 1 (default) = direct neighbors only. 2 = also includes neighbors-of-neighbors. Max 2"`

	// Fields for action=shortest_path
	SourceID string `json:"source_id,omitempty" jsonschema:"description=Starting entity ID for shortest_path"`
	TargetID string `json:"target_id,omitempty" jsonschema:"description=Destination entity ID for shortest_path"`

	// Fields for action=batch
	Queries []BatchQuery `json:"queries,omitempty" jsonschema:"description=Array of sub-queries to run in parallel (required for action=batch and max 10). Each sub-query has its own action and parameters. Results are returned in the same order."`
}

// BatchQuery represents a single query within a batch request.
type BatchQuery struct {
	Action   string `json:"action" jsonschema:"description=Sub-query action: 'explore' (recommended) or 'neighbors' or 'get_entity' or 'shortest_path'. Note: 'batch' cannot be nested.,required,enum=neighbors,enum=get_entity,enum=shortest_path,enum=explore"`
	EntityID string `json:"entity_id,omitempty" jsonschema:"description=Entity ID to query (required for explore and neighbors and get_entity)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max neighbors to return (default 20)"`
	Depth    int    `json:"depth,omitempty" jsonschema:"description=Traversal depth for explore: 1 (default) or 2. Max 2"`
	SourceID string `json:"source_id,omitempty" jsonschema:"description=Source entity ID for shortest_path"`
	TargetID string `json:"target_id,omitempty" jsonschema:"description=Target entity ID for shortest_path"`
}

// GraphQueryResponse is the output for the graph_query tool.
type GraphQueryResponse struct {
	// For action=neighbors
	Neighbors []Neighbor `json:"neighbors,omitempty"`
	Count     int        `json:"count,omitempty"`

	// For action=get_entity
	Entity *Entity `json:"entity,omitempty"`

	// Shared: true when the query matched data, false when entity not found
	Found bool `json:"found"`

	// For action=shortest_path
	Path []string `json:"path,omitempty"`

	// For action=explore — contains root entity, all connected entities,
	// their relations, and neighbor details in a single response.
	Subgraph *Subgraph `json:"subgraph,omitempty"`

	// For action=batch — one result per sub-query, in the same order.
	BatchResults []BatchResult `json:"batch_results,omitempty"`
}

// Subgraph is an ego-graph (local subgraph) centered on a root entity.
// Contains the root, all connected entities within the traversal depth,
// the directed relations (edges) between them, and neighbor metadata.
// This structure is inspired by GraphRAG's local-search subgraph
// extraction pattern — returning a rich context in a single query.
type Subgraph struct {
	// Root is the central entity that was explored.
	Root Entity `json:"root"`
	// Entities are all other entities discovered during traversal (excludes root).
	Entities []Entity `json:"entities"`
	// Relations are all directed edges (subject→object) involving the root.
	Relations []Relation `json:"relations"`
	// Neighbors provides the same data as action=neighbors would, with
	// predicate and direction info for each connected entity.
	Neighbors []Neighbor `json:"neighbors"`
}

// BatchResult contains the result (or error) for one sub-query within a batch.
// Index corresponds to the position in the input queries array.
type BatchResult struct {
	Index    int                 `json:"index"`
	Action   string              `json:"action"`
	Response *GraphQueryResponse `json:"response,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// newGraphQueryTool creates the unified graph_query tool. Unexported because
// it is only used by ToolProvider within this package.
func newGraphQueryTool(store IStore) tool.Tool {
	t := &graphQueryTool{
		store:    store,
		explorer: &subgraphExplorer{store: store},
	}
	t.batcher = &batchExecutor{tool: t}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(GraphQueryToolName),
		function.WithDescription(
			"Query the knowledge graph to find entities, relationships, and paths. "+
				"**Start with action='explore'** — it returns the entity, ALL its neighbors (with attributes), "+
				"and ALL connecting relations in a SINGLE call. This replaces the need for separate get_entity + neighbors calls. "+
				"Example: {\"action\":\"explore\",\"entity_id\":\"appcd-dev\"} returns the full local subgraph. "+
				"**Use action='batch'** to query MULTIPLE entities at once — "+
				"e.g. {\"action\":\"batch\",\"queries\":[{\"action\":\"explore\",\"entity_id\":\"repo-1\"},{\"action\":\"explore\",\"entity_id\":\"repo-2\"}]}. "+
				"Other actions: 'get_entity' (single entity lookup), 'neighbors' (neighbor list only), "+
				"'shortest_path' (path between two entities). "+
				"EFFICIENCY: explore > get_entity+neighbors. batch > multiple separate calls.",
		),
	)
}

type graphQueryTool struct {
	store    IStore
	explorer *subgraphExplorer
	batcher  *batchExecutor
}

// execute routes the graph_query request to the appropriate handler.
func (t *graphQueryTool) execute(ctx context.Context, req GraphQueryRequest) (GraphQueryResponse, error) {
	switch req.Action {
	case "neighbors":
		return t.neighbors(ctx, req.EntityID, req.Limit)
	case "get_entity":
		return t.getEntity(ctx, req.EntityID)
	case "shortest_path":
		return t.shortestPath(ctx, req.SourceID, req.TargetID)
	case "explore":
		return t.explorer.Explore(ctx, req.EntityID, req.Limit, req.Depth)
	case "batch":
		return t.batcher.Execute(ctx, req.Queries)
	default:
		return GraphQueryResponse{}, fmt.Errorf(
			"%w: action must be 'neighbors', 'get_entity', 'shortest_path', 'explore', or 'batch', got %q",
			ErrInvalidInput, req.Action,
		)
	}
}

// neighbors returns one-hop neighbors of an entity.
// Found reflects entity existence, not neighbor count — an isolated entity
// with zero neighbors still returns Found: true.
func (t *graphQueryTool) neighbors(ctx context.Context, entityID string, limit int) (GraphQueryResponse, error) {
	if entityID == "" {
		return GraphQueryResponse{}, fmt.Errorf("%w: entity_id is required for action=neighbors", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 20
	}
	// Verify entity exists so Found accurately reflects "entity not in graph"
	// vs "entity exists but has no neighbors".
	entity, err := t.store.GetEntity(ctx, entityID)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	if entity == nil {
		return GraphQueryResponse{Found: false}, nil
	}
	neighbors, err := t.store.Neighbors(ctx, entityID, limit)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	return GraphQueryResponse{Neighbors: neighbors, Count: len(neighbors), Found: true}, nil
}

// getEntity looks up a single entity by ID.
func (t *graphQueryTool) getEntity(ctx context.Context, entityID string) (GraphQueryResponse, error) {
	if entityID == "" {
		return GraphQueryResponse{}, fmt.Errorf("%w: entity_id is required for action=get_entity", ErrInvalidInput)
	}
	entity, err := t.store.GetEntity(ctx, entityID)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	if entity == nil {
		return GraphQueryResponse{Found: false}, nil
	}
	return GraphQueryResponse{Entity: entity, Found: true}, nil
}

// shortestPath finds the shortest path between two entities.
func (t *graphQueryTool) shortestPath(ctx context.Context, sourceID, targetID string) (GraphQueryResponse, error) {
	if sourceID == "" || targetID == "" {
		return GraphQueryResponse{}, fmt.Errorf("%w: source_id and target_id are required for action=shortest_path", ErrInvalidInput)
	}
	path, err := t.store.ShortestPath(ctx, sourceID, targetID)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	return GraphQueryResponse{Path: path, Found: len(path) > 0}, nil
}

// ---- subgraphExplorer: ego-graph extraction ----

// subgraphExplorer handles the "explore" action, extracting an ego-graph
// (local subgraph) centered on a root entity. It uses errgroup to
// concurrently fetch the entity, neighbors, and relations, then assembles
// a deduplicated Subgraph response.
type subgraphExplorer struct {
	store IStore
}

// Explore returns an ego-graph subgraph centered on entityID.
// depth=1 (default) returns direct neighbors; depth=2 also includes
// neighbors-of-neighbors. All I/O is concurrent via errgroup.
func (e *subgraphExplorer) Explore(ctx context.Context, entityID string, limit, depth int) (GraphQueryResponse, error) {
	if entityID == "" {
		return GraphQueryResponse{}, fmt.Errorf("%w: entity_id is required for action=explore", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 20
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}

	// Fetch root entity, neighbors, and relations concurrently.
	var (
		rootEnt   *Entity
		neighbors []Neighbor
		outRels   []Relation
		inRels    []Relation
	)

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		rootEnt, err = e.store.GetEntity(gCtx, entityID)
		return err
	})
	g.Go(func() error {
		var err error
		neighbors, err = e.store.Neighbors(gCtx, entityID, limit)
		return err
	})
	g.Go(func() error {
		var err error
		outRels, err = e.store.RelationsOut(gCtx, entityID)
		return err
	})
	g.Go(func() error {
		var err error
		inRels, err = e.store.RelationsIn(gCtx, entityID)
		return err
	})

	if err := g.Wait(); err != nil {
		return GraphQueryResponse{}, err
	}

	if rootEnt == nil {
		return GraphQueryResponse{Found: false}, nil
	}

	// Assemble the subgraph from raw fetch results.
	sg := e.assemble(rootEnt, neighbors, outRels, inRels)

	// Depth 2: expand by fetching neighbors of neighbors.
	if depth >= 2 {
		if err := e.expandDepth2(ctx, entityID, limit, neighbors, sg); err != nil {
			return GraphQueryResponse{}, err
		}
	}

	return GraphQueryResponse{
		Subgraph: sg,
		Found:    true,
		Count:    len(sg.Entities),
	}, nil
}

// assemble builds a deduplicated Subgraph from raw fetch results.
func (e *subgraphExplorer) assemble(root *Entity, neighbors []Neighbor, outRels, inRels []Relation) *Subgraph {
	// Deduplicate entities.
	entityMap := map[string]Entity{root.ID: *root}
	for _, n := range neighbors {
		entityMap[n.Entity.ID] = n.Entity
	}

	// Deduplicate relations by triple key.
	relMap := make(map[string]Relation, len(outRels)+len(inRels))
	for _, r := range outRels {
		relMap[r.SubjectID+":"+r.Predicate+":"+r.ObjectID] = r
	}
	for _, r := range inRels {
		relMap[r.SubjectID+":"+r.Predicate+":"+r.ObjectID] = r
	}

	// Flatten to slices (root is stored separately).
	entities := make([]Entity, 0, len(entityMap)-1)
	for _, ent := range entityMap {
		if ent.ID != root.ID {
			entities = append(entities, ent)
		}
	}
	relations := make([]Relation, 0, len(relMap))
	for _, r := range relMap {
		relations = append(relations, r)
	}

	return &Subgraph{
		Root:      *root,
		Entities:  entities,
		Relations: relations,
		Neighbors: neighbors,
	}
}

// expandDepth2 fetches neighbors-of-neighbors concurrently and merges
// them into the existing subgraph. Uses errgroup for clean error handling
// and a mutex to protect concurrent writes to the subgraph's shared slices.
func (e *subgraphExplorer) expandDepth2(
	ctx context.Context, rootID string, limit int,
	hop1 []Neighbor, sg *Subgraph,
) error {
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)

	for _, n := range hop1 {
		nID := n.Entity.ID
		if nID == rootID {
			continue // skip self-loops
		}
		g.Go(func() error {
			hop2, err := e.store.Neighbors(gCtx, nID, limit)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			for _, h := range hop2 {
				sg.Neighbors = append(sg.Neighbors, h)
				// Only add to Entities if not already present.
				found := false
				if h.Entity.ID == sg.Root.ID {
					found = true
				}
				if !found {
					for _, existing := range sg.Entities {
						if existing.ID == h.Entity.ID {
							found = true
							break
						}
					}
				}
				if !found {
					sg.Entities = append(sg.Entities, h.Entity)
				}
			}
			return nil
		})
	}

	return g.Wait()
}

// ---- batchExecutor: parallel multi-query execution ----

// batchExecutor handles the "batch" action, running multiple sub-queries
// concurrently and collecting results. Each sub-query's error is captured
// in its BatchResult rather than aborting the whole batch.
type batchExecutor struct {
	tool *graphQueryTool
}

// maxBatchSize is the maximum number of sub-queries in a batch request.
const maxBatchSize = 10

// Execute runs all sub-queries concurrently and returns their results.
// Individual sub-query errors are captured per-result (one failure doesn't
// abort others). The only errors returned at the batch level are
// validation errors (empty queries, batch too large).
func (b *batchExecutor) Execute(ctx context.Context, queries []BatchQuery) (GraphQueryResponse, error) {
	if len(queries) == 0 {
		return GraphQueryResponse{}, fmt.Errorf(
			"%w: queries array is required and must not be empty for action=batch",
			ErrInvalidInput,
		)
	}
	if len(queries) > maxBatchSize {
		return GraphQueryResponse{}, fmt.Errorf(
			"%w: batch size %d exceeds maximum of %d",
			ErrInvalidInput, len(queries), maxBatchSize,
		)
	}

	results := make([]BatchResult, len(queries))

	// Use errgroup only for coordination, not for error propagation —
	// individual sub-query errors are captured in BatchResult.Error.
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(idx int, query BatchQuery) {
			defer wg.Done()
			resp, err := b.dispatch(ctx, query)
			br := BatchResult{Index: idx, Action: query.Action}
			if err != nil {
				br.Error = err.Error()
			} else {
				br.Response = &resp
			}
			results[idx] = br
		}(i, q)
	}
	wg.Wait()

	return GraphQueryResponse{BatchResults: results, Found: true}, nil
}

// dispatch routes a single batch sub-query to the correct handler.
func (b *batchExecutor) dispatch(ctx context.Context, q BatchQuery) (GraphQueryResponse, error) {
	switch q.Action {
	case "neighbors":
		return b.tool.neighbors(ctx, q.EntityID, q.Limit)
	case "get_entity":
		return b.tool.getEntity(ctx, q.EntityID)
	case "shortest_path":
		return b.tool.shortestPath(ctx, q.SourceID, q.TargetID)
	case "explore":
		return b.tool.explorer.Explore(ctx, q.EntityID, q.Limit, q.Depth)
	default:
		return GraphQueryResponse{}, fmt.Errorf("unsupported batch sub-action %q", q.Action)
	}
}
