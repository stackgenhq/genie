// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/klauspost/compress/zstd"
)

const snapshotFileZst = "memory.bin.zst"

// edgePredicates is the type stored in edge Properties.Data for (subject, object)
// to support multiple predicates per pair. Persisted as part of snapshot; keep gob-encodable.
type edgePredicates []string

// InMemoryStore implements IStore using github.com/dominikbraun/graph for the
// in-memory structure and algorithms. Persistence is gob+zstd to memory.bin.zst.
// Use ShortestPath for path finding between entities.
type InMemoryStore struct {
	mu         sync.RWMutex
	g          graph.Graph[string, Entity]
	persistDir string
}

// InMemoryStoreOption configures the in-memory store.
type InMemoryStoreOption func(*InMemoryStore)

// WithPersistenceDir sets the directory for the snapshot file (e.g. ~/.genie/<agent>).
// When set, load is called from NewInMemoryStore and save is called on Close and after writes.
func WithPersistenceDir(dir string) InMemoryStoreOption {
	return func(s *InMemoryStore) {
		s.persistDir = dir
	}
}

// entityHash returns the entity ID for use as the graph vertex hash.
func entityHash(e Entity) string {
	return e.ID
}

// NewInMemoryStore creates an in-memory graph store backed by dominikbraun/graph.
// When persistence dir is set, state is loaded from memory.bin.zst if present and
// saved on Add and Close (gob+zstd only).
func NewInMemoryStore(opts ...InMemoryStoreOption) (*InMemoryStore, error) {
	g := graph.New(entityHash, graph.Directed())
	s := &InMemoryStore{g: g}
	for _, opt := range opts {
		opt(s)
	}
	if s.persistDir != "" {
		if err := s.load(context.Background()); err != nil {
			return nil, fmt.Errorf("load snapshot: %w", err)
		}
	}
	return s, nil
}

func (s *InMemoryStore) AddEntity(ctx context.Context, e Entity) error {
	if e.ID == "" || e.Type == "" {
		return fmt.Errorf("entity id and type required: %w", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	attrs := make(map[string]string, len(e.Attrs))
	for k, v := range e.Attrs {
		attrs[k] = v
	}
	entity := Entity{ID: e.ID, Type: e.Type, Attrs: attrs}
	// Overwrite if exists: remove incident edges, remove vertex, add vertex, re-add edges.
	if _, err := s.g.Vertex(e.ID); err == nil {
		if err := s.replaceVertex(e.ID, entity); err != nil {
			return fmt.Errorf("replace entity: %w", err)
		}
	} else {
		if err := s.g.AddVertex(entity); err != nil && !errors.Is(err, graph.ErrVertexAlreadyExists) {
			return fmt.Errorf("add entity: %w", err)
		}
	}
	return s.saveLocked(ctx)
}

// DeleteEntity removes an entity and all its incident edges from the graph.
// Returns nil if the entity does not exist (idempotent).
func (s *InMemoryStore) DeleteEntity(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("entity id required: %w", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// If vertex doesn't exist, treat as idempotent success.
	if _, err := s.g.Vertex(id); err != nil {
		if errors.Is(err, graph.ErrVertexNotFound) {
			return nil
		}
		return err
	}
	// Remove all incident edges before removing the vertex.
	adj, err := s.g.AdjacencyMap()
	if err != nil {
		return err
	}
	pred, err := s.g.PredecessorMap()
	if err != nil {
		return err
	}
	for target := range adj[id] {
		_ = s.g.RemoveEdge(id, target)
	}
	for source := range pred[id] {
		_ = s.g.RemoveEdge(source, id)
	}
	if err := s.g.RemoveVertex(id); err != nil {
		return fmt.Errorf("remove vertex: %w", err)
	}
	return s.saveLocked(ctx)
}

// DeleteRelation removes a specific predicate from the edge between subject and object.
// If the removed predicate was the last one on the edge, the edge itself is removed.
// Returns nil if the relation does not exist (idempotent).
func (s *InMemoryStore) DeleteRelation(ctx context.Context, r Relation) error {
	if r.SubjectID == "" || r.Predicate == "" || r.ObjectID == "" {
		return fmt.Errorf("relation subject_id, predicate, and object_id required: %w", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	edge, err := s.g.Edge(r.SubjectID, r.ObjectID)
	if err != nil {
		if errors.Is(err, graph.ErrEdgeNotFound) {
			return nil // idempotent
		}
		return fmt.Errorf("get edge: %w", err)
	}
	preds, _ := edge.Properties.Data.(edgePredicates)
	// Find and remove the target predicate.
	idx := -1
	for i, p := range preds {
		if p == r.Predicate {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil // predicate not found — idempotent
	}
	preds = append(preds[:idx], preds[idx+1:]...)
	if len(preds) == 0 {
		// Last predicate removed — remove the entire edge.
		if err := s.g.RemoveEdge(r.SubjectID, r.ObjectID); err != nil {
			return fmt.Errorf("remove edge: %w", err)
		}
	} else {
		if err := s.g.UpdateEdge(r.SubjectID, r.ObjectID, graph.EdgeData(preds)); err != nil {
			return fmt.Errorf("update edge: %w", err)
		}
	}
	return s.saveLocked(ctx)
}

// replaceVertex removes the vertex and all its incident edges, then re-adds the vertex and edges.
func (s *InMemoryStore) replaceVertex(id string, entity Entity) error {
	adj, err := s.g.AdjacencyMap()
	if err != nil {
		return err
	}
	pred, err := s.g.PredecessorMap()
	if err != nil {
		return err
	}
	var edgesToRestore []struct {
		source, target string
		preds          edgePredicates
	}
	for target, edge := range adj[id] {
		preds, _ := edge.Properties.Data.(edgePredicates)
		if preds == nil {
			preds = edgePredicates{}
		}
		edgesToRestore = append(edgesToRestore, struct {
			source, target string
			preds          edgePredicates
		}{id, target, preds})
	}
	for source := range pred[id] {
		if source == id {
			continue
		}
		edge, _ := s.g.Edge(source, id)
		preds, _ := edge.Properties.Data.(edgePredicates)
		if preds == nil {
			preds = edgePredicates{}
		}
		edgesToRestore = append(edgesToRestore, struct {
			source, target string
			preds          edgePredicates
		}{source, id, preds})
	}
	for _, e := range edgesToRestore {
		_ = s.g.RemoveEdge(e.source, e.target)
	}
	if err := s.g.RemoveVertex(id); err != nil {
		return err
	}
	if err := s.g.AddVertex(entity); err != nil {
		return err
	}
	for _, e := range edgesToRestore {
		if err := s.g.AddEdge(e.source, e.target, graph.EdgeData(edgePredicates(e.preds))); err != nil && !errors.Is(err, graph.ErrEdgeAlreadyExists) {
			return err
		}
	}
	return nil
}

func (s *InMemoryStore) AddRelation(ctx context.Context, r Relation) error {
	if r.SubjectID == "" || r.Predicate == "" || r.ObjectID == "" {
		return fmt.Errorf("relation subject_id, predicate, and object_id required: %w", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Ensure both endpoints exist (dominikbraun/graph requires vertices before edges).
	if _, err := s.g.Vertex(r.SubjectID); errors.Is(err, graph.ErrVertexNotFound) {
		_ = s.g.AddVertex(Entity{ID: r.SubjectID, Type: "unknown", Attrs: nil})
	}
	if _, err := s.g.Vertex(r.ObjectID); errors.Is(err, graph.ErrVertexNotFound) {
		_ = s.g.AddVertex(Entity{ID: r.ObjectID, Type: "unknown", Attrs: nil})
	}
	edge, err := s.g.Edge(r.SubjectID, r.ObjectID)
	if err != nil {
		if errors.Is(err, graph.ErrEdgeNotFound) {
			if errAdd := s.g.AddEdge(r.SubjectID, r.ObjectID, graph.EdgeData(edgePredicates{r.Predicate})); errAdd != nil {
				return fmt.Errorf("add relation: %w", errAdd)
			}
			return s.saveLocked(ctx)
		}
		return fmt.Errorf("get edge: %w", err)
	}
	preds, _ := edge.Properties.Data.(edgePredicates)
	if preds == nil {
		preds = edgePredicates{}
	}
	for _, p := range preds {
		if p == r.Predicate {
			return nil // idempotent
		}
	}
	preds = append(preds, r.Predicate)
	if err := s.g.UpdateEdge(r.SubjectID, r.ObjectID, graph.EdgeData(preds)); err != nil {
		return fmt.Errorf("update edge: %w", err)
	}
	return s.saveLocked(ctx)
}

func (s *InMemoryStore) GetEntity(ctx context.Context, id string) (*Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, err := s.g.Vertex(id)
	if err != nil {
		if errors.Is(err, graph.ErrVertexNotFound) {
			return nil, nil
		}
		return nil, err
	}
	attrs := make(map[string]string, len(e.Attrs))
	for k, v := range e.Attrs {
		attrs[k] = v
	}
	return &Entity{ID: e.ID, Type: e.Type, Attrs: attrs}, nil
}

func (s *InMemoryStore) RelationsOut(ctx context.Context, id string) ([]Relation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	adj, err := s.g.AdjacencyMap()
	if err != nil {
		return nil, err
	}
	out := adj[id]
	var rels []Relation
	for target, edge := range out {
		preds, _ := edge.Properties.Data.(edgePredicates)
		for _, p := range preds {
			rels = append(rels, Relation{SubjectID: id, Predicate: p, ObjectID: target})
		}
	}
	return rels, nil
}

func (s *InMemoryStore) RelationsIn(ctx context.Context, id string) ([]Relation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pred, err := s.g.PredecessorMap()
	if err != nil {
		return nil, err
	}
	in := pred[id]
	var rels []Relation
	for source, edge := range in {
		preds, _ := edge.Properties.Data.(edgePredicates)
		for _, p := range preds {
			rels = append(rels, Relation{SubjectID: source, Predicate: p, ObjectID: id})
		}
	}
	return rels, nil
}

func (s *InMemoryStore) Neighbors(ctx context.Context, id string, limit int) ([]Neighbor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	adj, err := s.g.AdjacencyMap()
	if err != nil {
		return nil, err
	}
	pred, err := s.g.PredecessorMap()
	if err != nil {
		return nil, err
	}
	var out []Neighbor
	// Dedupe by (entityID, outgoing) so the same entity can appear as both outgoing and incoming.
	seen := make(map[string]struct{})
	key := func(entityID string, outgoing bool) string {
		if outgoing {
			return entityID + ":true"
		}
		return entityID + ":false"
	}
	for target, edge := range adj[id] {
		if limit > 0 && len(out) >= limit {
			break
		}
		if _, ok := seen[key(target, true)]; ok {
			continue
		}
		seen[key(target, true)] = struct{}{}
		e, err := s.g.Vertex(target)
		if err != nil {
			continue
		}
		preds, _ := edge.Properties.Data.(edgePredicates)
		for _, p := range preds {
			attrs := make(map[string]string, len(e.Attrs))
			for k, v := range e.Attrs {
				attrs[k] = v
			}
			out = append(out, Neighbor{
				Entity:    Entity{ID: e.ID, Type: e.Type, Attrs: attrs},
				Predicate: p,
				Outgoing:  true,
			})
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	for source, edge := range pred[id] {
		if limit > 0 && len(out) >= limit {
			break
		}
		if _, ok := seen[key(source, false)]; ok {
			continue
		}
		seen[key(source, false)] = struct{}{}
		e, err := s.g.Vertex(source)
		if err != nil {
			continue
		}
		preds, _ := edge.Properties.Data.(edgePredicates)
		for _, p := range preds {
			attrs := make(map[string]string, len(e.Attrs))
			for k, v := range e.Attrs {
				attrs[k] = v
			}
			out = append(out, Neighbor{
				Entity:    Entity{ID: e.ID, Type: e.Type, Attrs: attrs},
				Predicate: p,
				Outgoing:  false,
			})
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// ShortestPath returns the shortest path of entity IDs from source to target using
// unweighted edges. Returns nil path and nil error if no path exists. Returns an error
// if source or target vertex does not exist. Uses dominikbraun/graph's BFS-based path.
func (s *InMemoryStore) ShortestPath(ctx context.Context, sourceID, targetID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.g.Vertex(sourceID); err != nil {
		if errors.Is(err, graph.ErrVertexNotFound) {
			return nil, fmt.Errorf("source vertex %q not found: %w", sourceID, err)
		}
		return nil, err
	}
	if _, err := s.g.Vertex(targetID); err != nil {
		if errors.Is(err, graph.ErrVertexNotFound) {
			return nil, fmt.Errorf("target vertex %q not found: %w", targetID, err)
		}
		return nil, err
	}
	path, err := graph.ShortestPath(s.g, sourceID, targetID)
	if err != nil {
		if errors.Is(err, graph.ErrTargetNotReachable) {
			return nil, nil
		}
		return nil, err
	}
	return path, nil
}

// DeleteAll removes all entities and relations from the in-memory graph.
func (s *InMemoryStore) DeleteAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.g = graph.New(entityHash, graph.Directed())
	return s.saveLocked(ctx)
}

func (s *InMemoryStore) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(ctx)
}

func (s *InMemoryStore) saveLocked(ctx context.Context) error {
	if s.persistDir == "" {
		return nil
	}
	return s.save(ctx)
}

type snapshot struct {
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
}

func (s *InMemoryStore) save(ctx context.Context) error {
	if s.persistDir == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.persistDir, 0o755); err != nil {
		return fmt.Errorf("create persist dir: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snap, err := s.buildSnapshot()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.saveZst(snap)
}

func (s *InMemoryStore) buildSnapshot() (snapshot, error) {
	adj, err := s.g.AdjacencyMap()
	if err != nil {
		return snapshot{}, err
	}
	vertexIDs := make(map[string]struct{})
	for source, targets := range adj {
		vertexIDs[source] = struct{}{}
		for target := range targets {
			vertexIDs[target] = struct{}{}
		}
	}
	var entities []Entity
	var relations []Relation
	for id := range vertexIDs {
		e, err := s.g.Vertex(id)
		if err != nil {
			continue
		}
		entities = append(entities, e)
	}
	for source, targets := range adj {
		for target, edge := range targets {
			preds, _ := edge.Properties.Data.(edgePredicates)
			for _, p := range preds {
				relations = append(relations, Relation{SubjectID: source, Predicate: p, ObjectID: target})
			}
		}
	}
	return snapshot{Entities: entities, Relations: relations}, nil
}

var (
	zstdEncoder *zstd.Encoder
	zstdDecoder *zstd.Decoder
)

func init() {
	var err error
	zstdEncoder, err = zstd.NewWriter(nil, zstd.WithZeroFrames(true))
	if err != nil {
		panic("graph: init zstd encoder: " + err.Error())
	}
	zstdDecoder, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
	if err != nil {
		panic("graph: init zstd decoder: " + err.Error())
	}
}

func (s *InMemoryStore) saveZst(snap snapshot) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&snap); err != nil {
		return fmt.Errorf("gob encode snapshot: %w", err)
	}
	out := zstdEncoder.EncodeAll(buf.Bytes(), nil)
	path := filepath.Join(s.persistDir, snapshotFileZst)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return fmt.Errorf("write temporary snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename snapshot: %w", err)
	}
	return nil
}

func (s *InMemoryStore) load(ctx context.Context) error {
	if s.persistDir == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pathZst := filepath.Join(s.persistDir, snapshotFileZst)
	data, err := os.ReadFile(pathZst)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.loadZst(data)
}

func (s *InMemoryStore) loadZst(data []byte) error {
	decoded, err := zstdDecoder.DecodeAll(data, nil)
	if err != nil {
		return fmt.Errorf("zstd decode snapshot: %w", err)
	}
	var snap snapshot
	if err := gob.NewDecoder(bytes.NewReader(decoded)).Decode(&snap); err != nil {
		return fmt.Errorf("gob decode snapshot: %w", err)
	}
	return s.applySnapshot(snap)
}

func (s *InMemoryStore) applySnapshot(snap snapshot) error {
	for _, e := range snap.Entities {
		if err := s.g.AddVertex(e); err != nil && !errors.Is(err, graph.ErrVertexAlreadyExists) {
			return err
		}
	}
	// Group relations by (subject, object) so we add one edge per pair with multiple predicates.
	type key struct{ subject, object string }
	predsByEdge := make(map[key]edgePredicates)
	for _, r := range snap.Relations {
		k := key{r.SubjectID, r.ObjectID}
		predsByEdge[k] = append(predsByEdge[k], r.Predicate)
	}
	for k, preds := range predsByEdge {
		if err := s.g.AddEdge(k.subject, k.object, graph.EdgeData(preds)); err != nil && !errors.Is(err, graph.ErrEdgeAlreadyExists) {
			return err
		}
	}
	return nil
}
