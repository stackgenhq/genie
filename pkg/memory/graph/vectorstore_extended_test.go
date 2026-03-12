// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/graph"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
)

// These tests cover VectorBackedStore.Neighbors and VectorBackedStore.ShortestPath
// which were previously at 0% coverage.
var _ = Describe("VectorBackedStore extended coverage", func() {
	var (
		ctx       context.Context
		fakeStore *vectorfakes.FakeIStore
		vbs       *graph.VectorBackedStore
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeStore = &vectorfakes.FakeIStore{}
		var err error
		vbs, err = graph.NewVectorBackedStore(fakeStore)
		Expect(err).NotTo(HaveOccurred())
	})

	// Helper to create a relation search result.
	relResult := func(subj, pred, obj string) vector.SearchResult {
		rel := graph.Relation{SubjectID: subj, Predicate: pred, ObjectID: obj}
		relJSON, _ := json.Marshal(rel)
		return vector.SearchResult{
			Content: string(relJSON),
			Metadata: map[string]string{
				"__graph_type":     "relation",
				"graph_subject_id": subj,
				"graph_predicate":  pred,
				"graph_object_id":  obj,
			},
		}
	}

	// Helper to create an entity search result.
	entityResult := func(id, typ string) vector.SearchResult {
		e := graph.Entity{ID: id, Type: typ}
		eJSON, _ := json.Marshal(e)
		return vector.SearchResult{
			Content: string(eJSON),
			Metadata: map[string]string{
				"__graph_type":      "entity",
				"graph_entity_id":   id,
				"graph_entity_type": typ,
			},
		}
	}

	Describe("Neighbors", func() {
		It("returns neighbors from outgoing and incoming relations", func() {
			callCount := 0
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				callCount++
				// First two calls: RelationsOut + RelationsIn for the target entity
				if req.Filter["graph_subject_id"] == "alice" {
					return []vector.SearchResult{relResult("alice", "WORKS_ON", "proj-x")}, nil
				}
				if req.Filter["graph_object_id"] == "alice" {
					return []vector.SearchResult{relResult("bob", "MENTORS", "alice")}, nil
				}
				// Entity lookups for neighbors
				if req.Filter["graph_entity_id"] == "proj-x" {
					return []vector.SearchResult{entityResult("proj-x", "project")}, nil
				}
				if req.Filter["graph_entity_id"] == "bob" {
					return []vector.SearchResult{entityResult("bob", "person")}, nil
				}
				return nil, nil
			}

			neighbors, err := vbs.Neighbors(ctx, "alice", 20)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(2))

			// Verify direction
			for _, n := range neighbors {
				if n.Entity.ID == "proj-x" {
					Expect(n.Outgoing).To(BeTrue())
					Expect(n.Predicate).To(Equal("WORKS_ON"))
				}
				if n.Entity.ID == "bob" {
					Expect(n.Outgoing).To(BeFalse())
					Expect(n.Predicate).To(Equal("MENTORS"))
				}
			}
		})

		It("returns error when RelationsOut fails", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] != "" {
					return nil, fmt.Errorf("relations out error")
				}
				return nil, nil
			}

			_, err := vbs.Neighbors(ctx, "alice", 20)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("relations out error"))
		})

		It("returns error when RelationsIn fails", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] != "" {
					return nil, nil // RelationsOut succeeds with no results
				}
				if req.Filter["graph_object_id"] != "" {
					return nil, fmt.Errorf("relations in error")
				}
				return nil, nil
			}

			_, err := vbs.Neighbors(ctx, "alice", 20)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("relations in error"))
		})

		It("returns error when GetEntity fails for a neighbor", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] == "alice" {
					return []vector.SearchResult{relResult("alice", "OWNS", "repo-1")}, nil
				}
				if req.Filter["graph_object_id"] == "alice" {
					return nil, nil
				}
				if req.Filter["graph_entity_id"] == "repo-1" {
					return nil, fmt.Errorf("entity lookup failed")
				}
				return nil, nil
			}

			_, err := vbs.Neighbors(ctx, "alice", 20)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("entity lookup failed"))
		})

		It("creates unknown entity when neighbor entity not found", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] == "alice" {
					return []vector.SearchResult{relResult("alice", "OWNS", "orphan-entity")}, nil
				}
				if req.Filter["graph_object_id"] == "alice" {
					return nil, nil
				}
				// Entity lookup returns empty (not found)
				if req.Filter["graph_entity_id"] == "orphan-entity" {
					return nil, nil
				}
				return nil, nil
			}

			neighbors, err := vbs.Neighbors(ctx, "alice", 20)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(1))
			Expect(neighbors[0].Entity.ID).To(Equal("orphan-entity"))
			Expect(neighbors[0].Entity.Type).To(Equal("unknown"))
		})

		It("respects the limit parameter", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] == "alice" {
					return []vector.SearchResult{
						relResult("alice", "OWNS", "repo-1"),
						relResult("alice", "OWNS", "repo-2"),
						relResult("alice", "OWNS", "repo-3"),
					}, nil
				}
				if req.Filter["graph_object_id"] == "alice" {
					return nil, nil
				}
				// Entity lookups
				if req.Filter["graph_entity_id"] != "" {
					return []vector.SearchResult{entityResult(req.Filter["graph_entity_id"], "repo")}, nil
				}
				return nil, nil
			}

			neighbors, err := vbs.Neighbors(ctx, "alice", 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(2))
		})

		It("deduplicates neighbors by directional key", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] == "alice" {
					return []vector.SearchResult{
						relResult("alice", "OWNS", "repo-1"),
						relResult("alice", "OWNS", "repo-1"), // duplicate
					}, nil
				}
				if req.Filter["graph_object_id"] == "alice" {
					return nil, nil
				}
				if req.Filter["graph_entity_id"] == "repo-1" {
					return []vector.SearchResult{entityResult("repo-1", "repo")}, nil
				}
				return nil, nil
			}

			neighbors, err := vbs.Neighbors(ctx, "alice", 20)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(1)) // deduped
		})
	})

	Describe("ShortestPath", func() {
		It("returns single-element path when source == target", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_entity_id"] == "alice" {
					return []vector.SearchResult{entityResult("alice", "person")}, nil
				}
				return nil, nil
			}

			path, err := vbs.ShortestPath(ctx, "alice", "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal([]string{"alice"}))
		})

		It("returns error when source does not exist", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				return nil, nil // no results = entity not found
			}

			_, err := vbs.ShortestPath(ctx, "ghost", "alice")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("source vertex"))
		})

		It("returns error when target does not exist", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_entity_id"] == "alice" {
					return []vector.SearchResult{entityResult("alice", "person")}, nil
				}
				return nil, nil // target not found
			}

			_, err := vbs.ShortestPath(ctx, "alice", "ghost")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("target vertex"))
		})

		It("returns error when GetEntity fails for source", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_entity_id"] == "src" {
					return nil, fmt.Errorf("db error")
				}
				return nil, nil
			}

			_, err := vbs.ShortestPath(ctx, "src", "tgt")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("db error"))
		})

		It("finds a 2-hop path via BFS", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				// Entity lookups
				eid := req.Filter["graph_entity_id"]
				if eid == "alice" {
					return []vector.SearchResult{entityResult("alice", "person")}, nil
				}
				if eid == "bob" {
					return []vector.SearchResult{entityResult("bob", "person")}, nil
				}
				if eid == "repo" {
					return []vector.SearchResult{entityResult("repo", "repo")}, nil
				}

				// RelationsOut
				if req.Filter["graph_subject_id"] == "alice" {
					return []vector.SearchResult{relResult("alice", "MENTORS", "bob")}, nil
				}
				if req.Filter["graph_subject_id"] == "bob" {
					return []vector.SearchResult{relResult("bob", "OWNS", "repo")}, nil
				}

				// RelationsIn — return empty for simplicity
				if req.Filter["graph_object_id"] != "" {
					return nil, nil
				}
				return nil, nil
			}

			path, err := vbs.ShortestPath(ctx, "alice", "repo")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal([]string{"alice", "bob", "repo"}))
		})

		It("returns nil path for disconnected entities", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				eid := req.Filter["graph_entity_id"]
				if eid == "a" {
					return []vector.SearchResult{entityResult("a", "type")}, nil
				}
				if eid == "z" {
					return []vector.SearchResult{entityResult("z", "type")}, nil
				}
				// No relations
				return nil, nil
			}

			path, err := vbs.ShortestPath(ctx, "a", "z")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(BeNil())
		})

		It("returns error when BFS Neighbors fails", func() {
			callCount := 0
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				eid := req.Filter["graph_entity_id"]
				if eid == "a" {
					return []vector.SearchResult{entityResult("a", "type")}, nil
				}
				if eid == "b" {
					return []vector.SearchResult{entityResult("b", "type")}, nil
				}
				// First RelationsOut call succeeds, subsequent fail
				if req.Filter["graph_subject_id"] == "a" {
					callCount++
					if callCount <= 1 {
						return nil, fmt.Errorf("BFS neighbor error")
					}
				}
				return nil, nil
			}

			_, err := vbs.ShortestPath(ctx, "a", "b")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("BFS"))
		})
	})

	Describe("parseRelations fallback path", func() {
		It("falls back to JSON parsing when metadata is incomplete", func() {
			rel := graph.Relation{SubjectID: "a", Predicate: "OWNS", ObjectID: "b"}
			relJSON, _ := json.Marshal(rel)

			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] != "" {
					return []vector.SearchResult{
						{
							Content: string(relJSON),
							// Missing metadata — forces JSON fallback
							Metadata: map[string]string{},
						},
					}, nil
				}
				return nil, nil
			}

			rels, err := vbs.RelationsOut(ctx, "a")
			Expect(err).NotTo(HaveOccurred())
			Expect(rels).To(HaveLen(1))
			Expect(rels[0].SubjectID).To(Equal("a"))
		})

		It("returns error when JSON fallback fails", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] != "" {
					return []vector.SearchResult{
						{
							Content:  "not valid json",
							Metadata: map[string]string{},
						},
					}, nil
				}
				return nil, nil
			}

			_, err := vbs.RelationsOut(ctx, "a")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unmarshal"))
		})
	})
})
