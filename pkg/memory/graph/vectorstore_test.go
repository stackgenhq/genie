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

var _ = Describe("VectorBackedStore", func() {
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
		Expect(vbs).NotTo(BeNil())
	})

	Describe("NewVectorBackedStore", func() {
		It("returns error when vector store is nil", func() {
			_, err := graph.NewVectorBackedStore(nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("nil"))
		})
	})

	Describe("GetEntity", func() {
		It("returns nil for empty id without calling vector store", func() {
			entity, err := vbs.GetEntity(ctx, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(entity).To(BeNil())
			Expect(fakeStore.SearchCallCount()).To(Equal(0))
		})

		It("calls SearchWithFilter with empty query and metadata filter", func() {
			// Simulate the vector store returning a matching entity document.
			testEntity := graph.Entity{
				ID:    "org:appcd-dev",
				Type:  "organization",
				Attrs: map[string]string{"name": "AppCD Dev"},
			}
			entityJSON, err := json.Marshal(testEntity)
			Expect(err).NotTo(HaveOccurred())

			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				// The key assertion: query MUST be "" for ID-based lookups.
				Expect(req.Query).To(Equal(""))
				Expect(req.Filter).To(HaveKeyWithValue("__graph_type", "entity"))
				Expect(req.Filter).To(HaveKeyWithValue("graph_entity_id", "org:appcd-dev"))
				return []vector.SearchResult{
					{ID: "graph:entity:org:appcd-dev", Content: string(entityJSON)},
				}, nil
			}

			entity, err := vbs.GetEntity(ctx, "org:appcd-dev")
			Expect(err).NotTo(HaveOccurred())
			Expect(entity).NotTo(BeNil())
			Expect(entity.ID).To(Equal("org:appcd-dev"))
			Expect(entity.Type).To(Equal("organization"))
			Expect(entity.Attrs["name"]).To(Equal("AppCD Dev"))
			Expect(fakeStore.SearchCallCount()).To(Equal(1))
		})

		It("returns nil when entity is not found", func() {
			fakeStore.SearchReturns([]vector.SearchResult{}, nil)

			entity, err := vbs.GetEntity(ctx, "nonexistent")
			Expect(err).NotTo(HaveOccurred())
			Expect(entity).To(BeNil())
		})

		It("propagates vector store errors", func() {
			fakeStore.SearchReturns(nil, fmt.Errorf("connection lost"))

			entity, err := vbs.GetEntity(ctx, "some-id")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection lost"))
			Expect(entity).To(BeNil())
		})
	})

	Describe("AddEntity", func() {
		It("rejects entity with empty ID", func() {
			err := vbs.AddEntity(ctx, graph.Entity{ID: "", Type: "org"})
			Expect(err).To(HaveOccurred())
		})

		It("rejects entity with empty Type", func() {
			err := vbs.AddEntity(ctx, graph.Entity{ID: "e1", Type: ""})
			Expect(err).To(HaveOccurred())
		})

		It("stores entity via Upsert with correct metadata", func() {
			fakeStore.UpsertReturns(nil)

			err := vbs.AddEntity(ctx, graph.Entity{
				ID:    "svc:api",
				Type:  "service",
				Attrs: map[string]string{"port": "8080"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeStore.UpsertCallCount()).To(Equal(1))

			_, upsertReq := fakeStore.UpsertArgsForCall(0)
			Expect(upsertReq.Items).To(HaveLen(1))
			Expect(upsertReq.Items[0].ID).To(Equal("graph:Genie:entity:svc:api"))
			Expect(upsertReq.Items[0].Metadata["__graph_type"]).To(Equal("entity"))
			Expect(upsertReq.Items[0].Metadata["graph_entity_id"]).To(Equal("svc:api"))
			Expect(upsertReq.Items[0].Metadata["graph_entity_type"]).To(Equal("service"))
		})
	})

	Describe("AddRelation", func() {
		It("rejects relation with empty SubjectID", func() {
			err := vbs.AddRelation(ctx, graph.Relation{SubjectID: "", Predicate: "OWNS", ObjectID: "b"})
			Expect(err).To(HaveOccurred())
		})

		It("rejects relation with empty Predicate", func() {
			err := vbs.AddRelation(ctx, graph.Relation{SubjectID: "a", Predicate: "", ObjectID: "b"})
			Expect(err).To(HaveOccurred())
		})

		It("rejects relation with empty ObjectID", func() {
			err := vbs.AddRelation(ctx, graph.Relation{SubjectID: "a", Predicate: "OWNS", ObjectID: ""})
			Expect(err).To(HaveOccurred())
		})

		It("stores relation via Upsert with correct metadata", func() {
			fakeStore.UpsertReturns(nil)

			err := vbs.AddRelation(ctx, graph.Relation{
				SubjectID: "alice",
				Predicate: "OWNS",
				ObjectID:  "repo-x",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeStore.UpsertCallCount()).To(Equal(1))

			_, upsertReq := fakeStore.UpsertArgsForCall(0)
			Expect(upsertReq.Items).To(HaveLen(1))
			Expect(upsertReq.Items[0].ID).To(Equal("graph:Genie:relation:alice:OWNS:repo-x"))
			Expect(upsertReq.Items[0].Metadata["__graph_type"]).To(Equal("relation"))
			Expect(upsertReq.Items[0].Metadata["graph_subject_id"]).To(Equal("alice"))
			Expect(upsertReq.Items[0].Metadata["graph_predicate"]).To(Equal("OWNS"))
			Expect(upsertReq.Items[0].Metadata["graph_object_id"]).To(Equal("repo-x"))
		})
	})

	Describe("RelationsOut", func() {
		It("returns outgoing relations using metadata filter", func() {
			rel := graph.Relation{SubjectID: "a", Predicate: "USES", ObjectID: "b"}
			relJSON, _ := json.Marshal(rel)

			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_subject_id"] == "a" {
					return []vector.SearchResult{
						{
							Content: string(relJSON),
							Metadata: map[string]string{
								"graph_subject_id": "a",
								"graph_predicate":  "USES",
								"graph_object_id":  "b",
							},
						},
					}, nil
				}
				return nil, nil
			}

			rels, err := vbs.RelationsOut(ctx, "a")
			Expect(err).NotTo(HaveOccurred())
			Expect(rels).To(HaveLen(1))
			Expect(rels[0].SubjectID).To(Equal("a"))
			Expect(rels[0].Predicate).To(Equal("USES"))
			Expect(rels[0].ObjectID).To(Equal("b"))
		})
	})

	Describe("RelationsIn", func() {
		It("returns incoming relations using metadata filter", func() {
			fakeStore.SearchStub = func(
				_ context.Context, req vector.SearchRequest,
			) ([]vector.SearchResult, error) {
				if req.Filter["graph_object_id"] == "target" {
					return []vector.SearchResult{
						{
							Metadata: map[string]string{
								"graph_subject_id": "src",
								"graph_predicate":  "DEPENDS_ON",
								"graph_object_id":  "target",
							},
						},
					}, nil
				}
				return nil, nil
			}

			rels, err := vbs.RelationsIn(ctx, "target")
			Expect(err).NotTo(HaveOccurred())
			Expect(rels).To(HaveLen(1))
			Expect(rels[0].SubjectID).To(Equal("src"))
			Expect(rels[0].Predicate).To(Equal("DEPENDS_ON"))
		})
	})

	Describe("Close", func() {
		It("is a no-op and returns nil", func() {
			err := vbs.Close(ctx)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
