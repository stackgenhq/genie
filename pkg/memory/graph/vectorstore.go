package graph

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stackgenhq/genie/pkg/memory/vector"
)

// graphDocType is the metadata key used to distinguish graph documents from
// regular memory documents in the shared vector store collection.
const graphDocType = "__graph_type"

// graphTypeEntity and graphTypeRelation are the two values for graphDocType.
const (
	graphTypeEntity   = "entity"
	graphTypeRelation = "relation"
)

// metadata keys used for filter-based retrieval of graph documents.
const (
	metaEntityID   = "graph_entity_id"
	metaEntityType = "graph_entity_type"
	metaSubjectID  = "graph_subject_id"
	metaPredicate  = "graph_predicate"
	metaObjectID   = "graph_object_id"
)

// vectorStoreSearchLimit is the maximum number of results fetched per query.
// Graph queries scan all matching records so this needs to be large enough
// to cover the full set; external stores (Qdrant/Milvus) handle pagination.
const vectorStoreSearchLimit = 1000

// VectorBackedStore implements IStore on top of the existing vector.IStore
// (Qdrant, Milvus, or in-memory). Entities and relations are stored as
// vector documents with metadata discriminators, enabling the knowledge graph
// to reuse the same scalable storage backend as memory_search/memory_store.
//
// Trade-offs:
//   - ShortestPath uses iterative BFS built on Neighbors — acceptable for
//     typical agent-sized graphs (hundreds to low thousands of nodes).
//   - Embeddings for graph docs use the same embedder. The text stored is a
//     JSON representation of the entity/relation, enabling semantic search
//     over entities directly from memory_search.
type VectorBackedStore struct {
	vs vector.IStore
}

// NewVectorBackedStore creates a graph IStore that delegates to the given
// vector.IStore. The vector store must already be initialised and is owned
// by the caller (Close on VectorBackedStore is a no-op).
func NewVectorBackedStore(vs vector.IStore) (*VectorBackedStore, error) {
	if vs == nil {
		return nil, fmt.Errorf("vector store is nil; cannot create vector-backed graph store")
	}
	return &VectorBackedStore{vs: vs}, nil
}

// entityDocID returns the deterministic vector document ID for an entity.
func entityDocID(entityID string) string {
	return "graph:entity:" + entityID
}

// relationDocID returns the deterministic vector document ID for a relation triple.
func relationDocID(subjectID, predicate, objectID string) string {
	return fmt.Sprintf("graph:relation:%s:%s:%s", subjectID, predicate, objectID)
}

// AddEntity stores an entity. Upserts so that overwrites work correctly.
func (s *VectorBackedStore) AddEntity(ctx context.Context, e Entity) error {
	if e.ID == "" || e.Type == "" {
		return ErrInvalidInput
	}
	textBytes, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("failed to marshal entity: %w", err)
	}
	meta := map[string]string{
		graphDocType:   graphTypeEntity,
		metaEntityID:   e.ID,
		metaEntityType: e.Type,
	}
	return s.vs.Upsert(ctx, vector.BatchItem{
		ID:       entityDocID(e.ID),
		Text:     string(textBytes),
		Metadata: meta,
	})
}

// AddEntities stores multiple entities in a single batch, reducing the number
// of embedding API calls by passing them all to a single Upsert which in turn
// generates embeddings concurrently via errgroup. Use this instead of calling
// AddEntity in a loop when storing entities discovered in bulk (e.g. infra
// discovery, batch graph ingestion).
func (s *VectorBackedStore) AddEntities(ctx context.Context, entities []Entity) error {
	items := make([]vector.BatchItem, 0, len(entities))
	for _, e := range entities {
		if e.ID == "" || e.Type == "" {
			return ErrInvalidInput
		}
		textBytes, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("failed to marshal entity %s: %w", e.ID, err)
		}
		items = append(items, vector.BatchItem{
			ID:   entityDocID(e.ID),
			Text: string(textBytes),
			Metadata: map[string]string{
				graphDocType:   graphTypeEntity,
				metaEntityID:   e.ID,
				metaEntityType: e.Type,
			},
		})
	}
	return s.vs.Upsert(ctx, items...)
}

// AddRelation stores a directed relation. Upserts, so the same triple is idempotent.
func (s *VectorBackedStore) AddRelation(ctx context.Context, r Relation) error {
	if r.SubjectID == "" || r.Predicate == "" || r.ObjectID == "" {
		return ErrInvalidInput
	}
	textBytes, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to marshal relation: %w", err)
	}
	meta := map[string]string{
		graphDocType:  graphTypeRelation,
		metaSubjectID: r.SubjectID,
		metaPredicate: r.Predicate,
		metaObjectID:  r.ObjectID,
	}
	return s.vs.Upsert(ctx, vector.BatchItem{
		ID:       relationDocID(r.SubjectID, r.Predicate, r.ObjectID),
		Text:     string(textBytes),
		Metadata: meta,
	})
}

// GetEntity looks up an entity by ID using metadata filter.
func (s *VectorBackedStore) GetEntity(ctx context.Context, id string) (*Entity, error) {
	if id == "" {
		return nil, nil
	}
	results, err := s.vs.SearchWithFilter(ctx, "", vectorStoreSearchLimit, map[string]string{
		graphDocType: graphTypeEntity,
		metaEntityID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("vector store search for entity %q: %w", id, err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	var entity Entity
	if err := json.Unmarshal([]byte(results[0].Content), &entity); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity %q: %w", id, err)
	}
	return &entity, nil
}

// RelationsOut returns relations where subject_id equals id (outgoing edges).
func (s *VectorBackedStore) RelationsOut(ctx context.Context, id string) ([]Relation, error) {
	results, err := s.vs.SearchWithFilter(ctx, "", vectorStoreSearchLimit, map[string]string{
		graphDocType:  graphTypeRelation,
		metaSubjectID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("vector store search for outgoing relations of %q: %w", id, err)
	}
	return parseRelations(results)
}

// RelationsIn returns relations where object_id equals id (incoming edges).
func (s *VectorBackedStore) RelationsIn(ctx context.Context, id string) ([]Relation, error) {
	results, err := s.vs.SearchWithFilter(ctx, "", vectorStoreSearchLimit, map[string]string{
		graphDocType: graphTypeRelation,
		metaObjectID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("vector store search for incoming relations of %q: %w", id, err)
	}
	return parseRelations(results)
}

// Neighbors returns entities reachable in one hop from id (outgoing and incoming).
func (s *VectorBackedStore) Neighbors(ctx context.Context, id string, limit int) ([]Neighbor, error) {
	// Get outgoing relations.
	outRels, err := s.RelationsOut(ctx, id)
	if err != nil {
		return nil, err
	}
	// Get incoming relations.
	inRels, err := s.RelationsIn(ctx, id)
	if err != nil {
		return nil, err
	}

	var neighbors []Neighbor
	seen := make(map[string]bool)

	// Process outgoing: id -> objectID
	for _, r := range outRels {
		neighborID := r.ObjectID
		key := neighborID + ":" + r.Predicate + ":out"
		if seen[key] {
			continue
		}
		seen[key] = true
		entity, err := s.GetEntity(ctx, neighborID)
		if err != nil {
			return nil, fmt.Errorf("failed to get neighbor entity %q: %w", neighborID, err)
		}
		if entity == nil {
			entity = &Entity{ID: neighborID, Type: "unknown"}
		}
		neighbors = append(neighbors, Neighbor{
			Entity:    *entity,
			Predicate: r.Predicate,
			Outgoing:  true,
		})
		if limit > 0 && len(neighbors) >= limit {
			return neighbors, nil
		}
	}

	// Process incoming: subjectID -> id
	for _, r := range inRels {
		neighborID := r.SubjectID
		key := neighborID + ":" + r.Predicate + ":in"
		if seen[key] {
			continue
		}
		seen[key] = true
		entity, err := s.GetEntity(ctx, neighborID)
		if err != nil {
			return nil, fmt.Errorf("failed to get neighbor entity %q: %w", neighborID, err)
		}
		if entity == nil {
			entity = &Entity{ID: neighborID, Type: "unknown"}
		}
		neighbors = append(neighbors, Neighbor{
			Entity:    *entity,
			Predicate: r.Predicate,
			Outgoing:  false,
		})
		if limit > 0 && len(neighbors) >= limit {
			return neighbors, nil
		}
	}

	return neighbors, nil
}

// ShortestPath finds the shortest path between source and target using BFS
// over the Neighbors operation. Returns nil path and nil error if no path
// exists. Returns an error if source or target does not exist.
func (s *VectorBackedStore) ShortestPath(ctx context.Context, sourceID, targetID string) ([]string, error) {
	// Verify source exists.
	srcEntity, err := s.GetEntity(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to check source %q: %w", sourceID, err)
	}
	if srcEntity == nil {
		return nil, fmt.Errorf("source vertex %q does not exist", sourceID)
	}

	// Verify target exists.
	tgtEntity, err := s.GetEntity(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("failed to check target %q: %w", targetID, err)
	}
	if tgtEntity == nil {
		return nil, fmt.Errorf("target vertex %q does not exist", targetID)
	}

	if sourceID == targetID {
		return []string{sourceID}, nil
	}

	// BFS
	type bfsNode struct {
		id   string
		path []string
	}
	visited := map[string]bool{sourceID: true}
	queue := []bfsNode{{id: sourceID, path: []string{sourceID}}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		neighbors, err := s.Neighbors(ctx, current.id, 0) // 0 = no limit
		if err != nil {
			return nil, fmt.Errorf("BFS neighbors for %q: %w", current.id, err)
		}

		for _, n := range neighbors {
			nID := n.Entity.ID
			if visited[nID] {
				continue
			}
			visited[nID] = true
			newPath := make([]string, len(current.path)+1)
			copy(newPath, current.path)
			newPath[len(current.path)] = nID

			if nID == targetID {
				return newPath, nil
			}
			queue = append(queue, bfsNode{id: nID, path: newPath})
		}
	}

	return nil, nil // no path found
}

// Close is a no-op for the vector-backed store because the vector store
// lifecycle is managed by the caller (app.go closes vectorStore separately).
func (s *VectorBackedStore) Close(_ context.Context) error {
	return nil
}

// parseRelations converts vector search results into Relation slices.
func parseRelations(results []vector.SearchResult) ([]Relation, error) {
	relations := make([]Relation, 0, len(results))
	for _, r := range results {
		// Fast path: reconstruct from metadata without full JSON parse.
		subjectID := r.Metadata[metaSubjectID]
		predicate := r.Metadata[metaPredicate]
		objectID := r.Metadata[metaObjectID]
		if subjectID != "" && predicate != "" && objectID != "" {
			relations = append(relations, Relation{
				SubjectID: subjectID,
				Predicate: predicate,
				ObjectID:  objectID,
			})
			continue
		}
		// Fallback: parse JSON content.
		var rel Relation
		if err := json.Unmarshal([]byte(r.Content), &rel); err != nil {
			return nil, fmt.Errorf("failed to unmarshal relation from vector result: %w", err)
		}
		relations = append(relations, rel)
	}
	return relations, nil
}
