package vector_test

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/memory/vector"
)

var _ = Describe("Vector Store", func() {
	var (
		ctx   context.Context
		store *vector.Store
		err   error
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("in-memory (no persistence)", func() {
		BeforeEach(func() {
			cfg := vector.Config{
				EmbeddingProvider: "dummy",
			}
			store, err = cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(store).NotTo(BeNil())
		})

		It("should add and retrieve documents", func() {
			err = store.Add(ctx, vector.BatchItem{ID: "1", Text: "The sky is blue", Metadata: map[string]string{"type": "fact"}})
			Expect(err).NotTo(HaveOccurred())

			err = store.Add(ctx, vector.BatchItem{ID: "2", Text: "Apples are red", Metadata: map[string]string{"type": "fact"}})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "The sky is blue", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal("1"))
			Expect(results[0].Content).To(Equal("The sky is blue"))
			Expect(results[0].Score).To(BeNumerically(">", 0))
		})
	})

	Context("with persistence", func() {
		var tmpDir string

		BeforeEach(func() {
			tmpDir, err = os.MkdirTemp("", "genie_vector_test")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			os.RemoveAll(tmpDir)
		})

		It("should survive a restart by loading from snapshot", func() {
			// Create store 1, add a document.
			cfg := vector.Config{
				PersistenceDir:    tmpDir,
				EmbeddingProvider: "dummy",
			}
			store1, err := cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = store1.Add(ctx, vector.BatchItem{ID: "persist-1", Text: "Go is great", Metadata: map[string]string{"lang": "go"}})
			Expect(err).NotTo(HaveOccurred())

			// Create store 2 from the same directory — should restore.
			store2, err := cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())

			results, err := store2.Search(ctx, "Go is great", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal("persist-1"))
			Expect(results[0].Content).To(Equal("Go is great"))
		})
	})
})
