package vector_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/vector"
)

var _ = Describe("SearchWithFilter", func() {
	var (
		ctx   context.Context
		store *vector.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err = cfg.NewStore(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(store).NotTo(BeNil())
	})

	Context("filter-only mode (empty query with filters)", func() {
		BeforeEach(func() {
			// Pre-populate the store with documents that have metadata.
			err := store.Add(ctx,
				vector.BatchItem{
					ID:       "graph:entity:org:appcd-dev",
					Text:     `{"id":"org:appcd-dev","type":"organization"}`,
					Metadata: map[string]string{"__graph_type": "entity", "graph_entity_id": "org:appcd-dev"},
				},
				vector.BatchItem{
					ID:       "graph:entity:svc:api",
					Text:     `{"id":"svc:api","type":"service"}`,
					Metadata: map[string]string{"__graph_type": "entity", "graph_entity_id": "svc:api"},
				},
				vector.BatchItem{
					ID:       "graph:relation:org:appcd-dev:HAS:svc:api",
					Text:     `{"subject_id":"org:appcd-dev","predicate":"HAS","object_id":"svc:api"}`,
					Metadata: map[string]string{"__graph_type": "relation", "graph_subject_id": "org:appcd-dev"},
				},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns matching documents when query is empty and filter is provided", func() {
			results, err := store.SearchWithFilter(ctx, "", 10, map[string]string{
				"__graph_type":    "entity",
				"graph_entity_id": "org:appcd-dev",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal("graph:entity:org:appcd-dev"))
			Expect(results[0].Content).To(ContainSubstring("org:appcd-dev"))
		})

		It("returns empty results when filter matches nothing", func() {
			results, err := store.SearchWithFilter(ctx, "", 10, map[string]string{
				"__graph_type":    "entity",
				"graph_entity_id": "nonexistent",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("returns all entities when filtering by type only", func() {
			results, err := store.SearchWithFilter(ctx, "", 10, map[string]string{
				"__graph_type": "entity",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
		})

		It("returns relations when filtering for relation type", func() {
			results, err := store.SearchWithFilter(ctx, "", 10, map[string]string{
				"__graph_type": "relation",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Content).To(ContainSubstring("HAS"))
		})
	})

	Context("empty query guard", func() {
		It("returns error when both query and filter are empty", func() {
			results, err := store.SearchWithFilter(ctx, "", 10, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("non-empty query or at least one filter"))
			Expect(results).To(BeNil())
		})

		It("returns error when query is empty and filter is an empty map", func() {
			results, err := store.SearchWithFilter(ctx, "", 10, map[string]string{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("non-empty query or at least one filter"))
			Expect(results).To(BeNil())
		})
	})

	Context("vector search mode (non-empty query)", func() {
		BeforeEach(func() {
			err := store.Add(ctx,
				vector.BatchItem{ID: "v1", Text: "kubernetes pod restart", Metadata: map[string]string{"type": "k8s"}},
				vector.BatchItem{ID: "v2", Text: "aws s3 bucket policy", Metadata: map[string]string{"type": "aws"}},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("performs semantic search with non-empty query and no filter", func() {
			results, err := store.SearchWithFilter(ctx, "kubernetes pod restart", 5, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())
			Expect(results[0].ID).To(Equal("v1"))
		})

		It("performs semantic search with non-empty query and filter", func() {
			results, err := store.SearchWithFilter(ctx, "pod restart", 5, map[string]string{"type": "k8s"})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())
			Expect(results[0].Metadata["type"]).To(Equal("k8s"))
		})
	})

	Context("Search delegates to SearchWithFilter", func() {
		It("Search with empty string returns error (guard propagates)", func() {
			_, err := store.Search(ctx, "", 1)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("non-empty query or at least one filter"))
		})

		It("Search with non-empty string returns results normally", func() {
			err := store.Add(ctx, vector.BatchItem{ID: "s1", Text: "test doc"})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "test doc", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
		})
	})
})
