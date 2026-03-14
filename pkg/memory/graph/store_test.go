// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/graph"
)

var _ = Describe("InMemoryStore", func() {
	var ctx context.Context
	var store *graph.InMemoryStore
	var tmpDir string

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "graph-store-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if store != nil {
			_ = store.Close(ctx)
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("when created without persistence", func() {
		BeforeEach(func() {
			var err error
			store, err = graph.NewInMemoryStore()
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns ErrInvalidInput for entity with empty ID or Type", func(ctx context.Context) {
			err := store.AddEntity(ctx, graph.Entity{ID: "", Type: "person"})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, graph.ErrInvalidInput)).To(BeTrue())
			err = store.AddEntity(ctx, graph.Entity{ID: "e1", Type: ""})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, graph.ErrInvalidInput)).To(BeTrue())
		})

		It("returns ErrInvalidInput for relation with empty subject_id, predicate, or object_id", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "a", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "b", Type: "issue"})).To(Succeed())
			err := store.AddRelation(ctx, graph.Relation{SubjectID: "", Predicate: "P", ObjectID: "b"})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, graph.ErrInvalidInput)).To(BeTrue())
			err = store.AddRelation(ctx, graph.Relation{SubjectID: "a", Predicate: "", ObjectID: "b"})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, graph.ErrInvalidInput)).To(BeTrue())
			err = store.AddRelation(ctx, graph.Relation{SubjectID: "a", Predicate: "P", ObjectID: ""})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, graph.ErrInvalidInput)).To(BeTrue())
		})

		It("adds and retrieves an entity", func(ctx context.Context) {
			e := graph.Entity{ID: "e1", Type: "person", Attrs: map[string]string{"name": "Alice"}}
			Expect(store.AddEntity(ctx, e)).To(Succeed())
			got, err := store.GetEntity(ctx, "e1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.ID).To(Equal("e1"))
			Expect(got.Type).To(Equal("person"))
			Expect(got.Attrs["name"]).To(Equal("Alice"))
		})

		It("returns nil for missing entity", func(ctx context.Context) {
			got, err := store.GetEntity(ctx, "missing")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("Close succeeds when persistence dir is not set", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "c", Type: "person"})).To(Succeed())
			Expect(store.Close(ctx)).To(Succeed())
		})

		It("overwrites entity when adding same id again", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "ov", Type: "person", Attrs: map[string]string{"a": "1"}})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "ov", Type: "project", Attrs: map[string]string{"b": "2"}})).To(Succeed())
			got, err := store.GetEntity(ctx, "ov")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Type).To(Equal("project"))
			Expect(got.Attrs).To(HaveLen(1))
			Expect(got.Attrs["b"]).To(Equal("2"))
		})

		It("returns a copy so caller cannot mutate stored entity", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "copy", Type: "person", Attrs: map[string]string{"k": "v"}})).To(Succeed())
			got1, err := store.GetEntity(ctx, "copy")
			Expect(err).NotTo(HaveOccurred())
			Expect(got1.Attrs).NotTo(BeNil())
			got1.Attrs["k"] = "mutated"
			got2, err := store.GetEntity(ctx, "copy")
			Expect(err).NotTo(HaveOccurred())
			Expect(got2.Attrs["k"]).To(Equal("v"))
		})

		It("adds relation and returns neighbors", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "a", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "b", Type: "issue"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "a", Predicate: "WORKED_ON", ObjectID: "b"})).To(Succeed())
			neighbors, err := store.Neighbors(ctx, "a", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(1))
			Expect(neighbors[0].Entity.ID).To(Equal("b"))
			Expect(neighbors[0].Predicate).To(Equal("WORKED_ON"))
			Expect(neighbors[0].Outgoing).To(BeTrue())
		})

		It("returns shortest path between two entities", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "x", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "y", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "z", Type: "person"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "x", Predicate: "KNOWS", ObjectID: "y"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "y", Predicate: "KNOWS", ObjectID: "z"})).To(Succeed())
			path, err := store.ShortestPath(ctx, "x", "z")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal([]string{"x", "y", "z"}))
		})

		It("returns nil path when target is unreachable", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "alone", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "other", Type: "person"})).To(Succeed())
			path, err := store.ShortestPath(ctx, "alone", "other")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(BeNil())
		})

		It("returns single-vertex path when source equals target", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "self", Type: "person"})).To(Succeed())
			path, err := store.ShortestPath(ctx, "self", "self")
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal([]string{"self"}))
		})

		It("returns error when ShortestPath source or target does not exist", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "only", Type: "person"})).To(Succeed())
			_, err := store.ShortestPath(ctx, "missing", "only")
			Expect(err).To(HaveOccurred())
			_, err = store.ShortestPath(ctx, "only", "missing")
			Expect(err).To(HaveOccurred())
		})

		It("returns RelationsIn for entity as object", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "subj", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "obj", Type: "issue"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "subj", Predicate: "OWNS", ObjectID: "obj"})).To(Succeed())
			in, err := store.RelationsIn(ctx, "obj")
			Expect(err).NotTo(HaveOccurred())
			Expect(in).To(HaveLen(1))
			Expect(in[0].SubjectID).To(Equal("subj"))
			Expect(in[0].Predicate).To(Equal("OWNS"))
			Expect(in[0].ObjectID).To(Equal("obj"))
		})

		It("Neighbors respects limit", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "center", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "n1", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "n2", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "n3", Type: "person"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "center", Predicate: "LINK", ObjectID: "n1"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "center", Predicate: "LINK", ObjectID: "n2"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "center", Predicate: "LINK", ObjectID: "n3"})).To(Succeed())
			neighbors, err := store.Neighbors(ctx, "center", 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(2))
		})

		It("Neighbors includes incoming relations with Outgoing false", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "incoming_a", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "hub", Type: "person"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "incoming_a", Predicate: "POINTS_TO", ObjectID: "hub"})).To(Succeed())
			neighbors, err := store.Neighbors(ctx, "hub", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(1))
			Expect(neighbors[0].Entity.ID).To(Equal("incoming_a"))
			Expect(neighbors[0].Outgoing).To(BeFalse())
			Expect(neighbors[0].Predicate).To(Equal("POINTS_TO"))
		})

		It("AddRelation is idempotent for same triple", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "s", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "o", Type: "person"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "s", Predicate: "SAME", ObjectID: "o"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "s", Predicate: "SAME", ObjectID: "o"})).To(Succeed())
			out, err := store.RelationsOut(ctx, "s")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(1))
		})

		It("stores multiple predicates between same subject and object", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "multi_s", Type: "person"})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "multi_o", Type: "issue"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "multi_s", Predicate: "WORKED_ON", ObjectID: "multi_o"})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "multi_s", Predicate: "REPORTS", ObjectID: "multi_o"})).To(Succeed())
			out, err := store.RelationsOut(ctx, "multi_s")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(2))
			preds := []string{out[0].Predicate, out[1].Predicate}
			Expect(preds).To(ContainElement("WORKED_ON"))
			Expect(preds).To(ContainElement("REPORTS"))
		})
	})

	Context("when created with persistence dir", func() {
		BeforeEach(func() {
			var err error
			store, err = graph.NewInMemoryStore(
				graph.WithPersistenceDir(tmpDir),
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("loads empty store when snapshot file does not exist", func(ctx context.Context) {
			got, err := store.GetEntity(ctx, "any")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
			Expect(store.AddEntity(ctx, graph.Entity{ID: "first", Type: "person"})).To(Succeed())
			got, err = store.GetEntity(ctx, "first")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
		})

		It("persists isolated vertex (no edges) and reloads", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "loner", Type: "person", Attrs: map[string]string{"name": "Solo"}})).To(Succeed())
			Expect(store.Close(ctx)).To(Succeed())
			store = nil
			store2, err := graph.NewInMemoryStore(graph.WithPersistenceDir(tmpDir))
			Expect(err).NotTo(HaveOccurred())
			store = store2
			got, err := store.GetEntity(ctx, "loner")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Type).To(Equal("person"))
			Expect(got.Attrs["name"]).To(Equal("Solo"))
		})

		It("returns error when loading from corrupt snapshot file", func(ctx context.Context) {
			corruptPath := filepath.Join(tmpDir, "memory.bin.zst")
			Expect(os.WriteFile(corruptPath, []byte("not valid zst or gob"), 0o600)).To(Succeed())
			_, err := graph.NewInMemoryStore(graph.WithPersistenceDir(tmpDir))
			Expect(err).To(HaveOccurred())
			store = nil
		})

		It("persists to memory.bin.zst and reloads", func(ctx context.Context) {
			Expect(store.AddEntity(ctx, graph.Entity{ID: "c1", Type: "person", Attrs: map[string]string{"name": "Bob"}})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{SubjectID: "c1", Predicate: "OWNS", ObjectID: "c2"})).To(Succeed())
			Expect(store.Close(ctx)).To(Succeed())
			store = nil

			Expect(filepath.Join(tmpDir, "memory.bin.zst")).To(BeAnExistingFile())

			store2, err := graph.NewInMemoryStore(
				graph.WithPersistenceDir(tmpDir),
			)
			Expect(err).NotTo(HaveOccurred())
			store = store2
			got, err := store.GetEntity(ctx, "c1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Attrs["name"]).To(Equal("Bob"))
			out, err := store.RelationsOut(ctx, "c1")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(1))
			Expect(out[0].ObjectID).To(Equal("c2"))
		})

		It("full lifecycle against temp dir: empty → add → retrieve → update → retrieve → close/reopen → retrieve", func(ctx context.Context) {
			By("Start with empty store (persistence dir has no snapshot yet)")
			got, err := store.GetEntity(ctx, "any")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())

			By("Add entities and a relation")
			Expect(store.AddEntity(ctx, graph.Entity{
				ID: "alice", Type: "person",
				Attrs: map[string]string{"name": "Alice", "role": "dev"},
			})).To(Succeed())
			Expect(store.AddEntity(ctx, graph.Entity{
				ID: "proj-x", Type: "project",
				Attrs: map[string]string{"name": "Project X"},
			})).To(Succeed())
			Expect(store.AddRelation(ctx, graph.Relation{
				SubjectID: "alice", Predicate: "WORKS_ON", ObjectID: "proj-x",
			})).To(Succeed())

			By("Retrieve and assert initial state")
			got, err = store.GetEntity(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Type).To(Equal("person"))
			Expect(got.Attrs["name"]).To(Equal("Alice"))
			Expect(got.Attrs["role"]).To(Equal("dev"))
			neighbors, err := store.Neighbors(ctx, "alice", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(1))
			Expect(neighbors[0].Entity.ID).To(Equal("proj-x"))
			Expect(neighbors[0].Predicate).To(Equal("WORKS_ON"))

			By("Update entity (overwrite same id with new attrs)")
			Expect(store.AddEntity(ctx, graph.Entity{
				ID: "alice", Type: "person",
				Attrs: map[string]string{"name": "Alice Smith", "role": "lead"},
			})).To(Succeed())

			By("Retrieve and assert updated state")
			got, err = store.GetEntity(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Attrs["name"]).To(Equal("Alice Smith"))
			Expect(got.Attrs["role"]).To(Equal("lead"))
			neighbors, err = store.Neighbors(ctx, "alice", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(1))

			By("Close store and reopen from same dir (persist and reload)")
			Expect(store.Close(ctx)).To(Succeed())
			Expect(filepath.Join(tmpDir, "memory.bin.zst")).To(BeAnExistingFile())
			store = nil
			store2, err := graph.NewInMemoryStore(graph.WithPersistenceDir(tmpDir))
			Expect(err).NotTo(HaveOccurred())
			store = store2

			By("Retrieve after reload and assert persisted state")
			got, err = store.GetEntity(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.Type).To(Equal("person"))
			Expect(got.Attrs["name"]).To(Equal("Alice Smith"))
			Expect(got.Attrs["role"]).To(Equal("lead"))
			gotProj, err := store.GetEntity(ctx, "proj-x")
			Expect(err).NotTo(HaveOccurred())
			Expect(gotProj).NotTo(BeNil())
			out, err := store.RelationsOut(ctx, "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(1))
			Expect(out[0].ObjectID).To(Equal("proj-x"))
		})
	})
})

var _ = Describe("ToolProvider", func() {
	It("returns no tools when store is nil", func() {
		provider := graph.NewToolProvider(nil)
		tools := provider.GetTools(context.Background())
		Expect(tools).To(BeNil())
	})

	It("returns two tools when store is non-nil", func() {
		store, err := graph.NewInMemoryStore()
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close(context.Background()) }()
		provider := graph.NewToolProvider(store)
		tools := provider.GetTools(context.Background())
		Expect(tools).To(HaveLen(2))
		names := make([]string, len(tools))
		for i, t := range tools {
			names[i] = t.Declaration().Name
		}
		Expect(names).To(ContainElement("graph_store"))
		Expect(names).To(ContainElement("graph_query"))
	})
})
