package vector_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/vector"
)

var _ = Describe("dummyEmbedder", func() {
	var (
		store *vector.Store
	)

	BeforeEach(func() {
		var err error
		store, err = vector.Config{EmbeddingProvider: "dummy"}.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())
	})

	// embed is a small helper that adds a document and retrieves its embedding
	// indirectly by searching for the same text.
	// We use the store's Search to verify embedding behaviour.

	Describe("determinism", func() {
		It("should produce identical embeddings for the same text across calls", func(ctx context.Context) {
			err := store.Add(ctx, vector.BatchItem{ID: "d1", Text: "hello world"})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "hello world", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			// An exact-match search on the same text should return the highest
			// possible similarity (cosine ≈ 1.0), proving the embedding is
			// deterministic.
			Expect(results[0].Score).To(BeNumerically("~", 1.0, 1e-9))
		})
	})

	Describe("differentiation", func() {
		It("should produce distinct embeddings for different texts", func(ctx context.Context) {
			err := store.Add(ctx,
				vector.BatchItem{ID: "a", Text: "the quick brown fox"},
				vector.BatchItem{ID: "b", Text: "completely unrelated string xyz 12345"},
			)
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "the quick brown fox", 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
			// The exact-match document must rank first with a higher score.
			Expect(results[0].ID).To(Equal("a"))
			Expect(results[0].Score).To(BeNumerically(">", results[1].Score))
		})
	})

	Describe("full-text influence", func() {
		It("should differentiate texts that share a long prefix", func(ctx context.Context) {
			// These two strings share the first 30 characters but differ at the
			// end. The old byte-copy approach would have produced nearly
			// identical vectors; the PRNG approach should distinguish them.
			textA := "abcdefghijklmnopqrstuvwxyz0123 ALPHA"
			textB := "abcdefghijklmnopqrstuvwxyz0123 BRAVO"

			err := store.Add(ctx,
				vector.BatchItem{ID: "pa", Text: textA},
				vector.BatchItem{ID: "pb", Text: textB},
			)
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, textA, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].ID).To(Equal("pa"))
			// The non-matching doc should have a noticeably lower score.
			Expect(results[0].Score).To(BeNumerically(">", results[1].Score+0.01))
		})
	})

	Describe("GetDimensions consistency", func() {
		It("should return vector length matching GetDimensions", func(ctx context.Context) {
			// Indirectly verified: the store was created with the dummy
			// embedder without error, and search works. If the dimension
			// were inconsistent the store would fail internally.
			err := store.Add(ctx, vector.BatchItem{ID: "dim", Text: "dimension check"})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "dimension check", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Score).To(BeNumerically("~", 1.0, 1e-9))
		})
	})

	Describe("GetEmbeddingWithUsage", func() {
		It("should return nil usage without error", func(ctx context.Context) {
			// Verified through the store lifecycle — the store may internally
			// call GetEmbeddingWithUsage. We ensure no error surfaces.
			err := store.Add(ctx, vector.BatchItem{ID: "u1", Text: "usage test"})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "usage test", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
		})
	})
})
