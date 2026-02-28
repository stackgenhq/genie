package vector_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
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

		It("should delete documents by ID", func() {
			err = store.Add(ctx, vector.BatchItem{ID: "del-1", Text: "To be deleted", Metadata: map[string]string{"type": "temp"}})
			Expect(err).NotTo(HaveOccurred())

			err = store.Delete(ctx, "del-1")
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "To be deleted", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should upsert by replacing existing document with same ID", func() {
			err = store.Add(ctx, vector.BatchItem{ID: "upsert-1", Text: "Original content", Metadata: map[string]string{"v": "1"}})
			Expect(err).NotTo(HaveOccurred())

			err = store.Upsert(ctx, vector.BatchItem{ID: "upsert-1", Text: "Updated content", Metadata: map[string]string{"v": "2"}})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, "Updated content", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal("upsert-1"))
			Expect(results[0].Content).To(Equal("Updated content"))
			Expect(results[0].Metadata["v"]).To(Equal("2"))
		})

		It("should close without error", func() {
			err = store.Close(ctx)
			Expect(err).NotTo(HaveOccurred())
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

		It("should delete and persist the deletion on restart", func() {
			cfg := vector.Config{
				PersistenceDir:    tmpDir,
				EmbeddingProvider: "dummy",
			}
			s1, err := cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())

			err = s1.Add(ctx, vector.BatchItem{ID: "del-test", Text: "deletable", Metadata: map[string]string{}})
			Expect(err).NotTo(HaveOccurred())

			err = s1.Delete(ctx, "del-test")
			Expect(err).NotTo(HaveOccurred())

			err = s1.Close(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Reload — deleted item should be gone
			s2, err := cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())
			results, err := s2.Search(ctx, "deletable", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})
	})
})

var _ = Describe("DefaultConfig", func() {
	It("should return config with dummy embedding provider", func() {
		cfg := vector.DefaultConfig(context.Background(), security.NewEnvProvider())
		Expect(cfg.EmbeddingProvider).To(Equal("dummy"))
	})
})

var _ = Describe("SearchResult.String", func() {
	It("should return LLM-friendly format", func() {
		sr := vector.SearchResult{
			ID:       "id-1",
			Content:  "Some content",
			Score:    0.95,
			Metadata: map[string]string{"type": "fact"},
		}
		s := sr.String()
		Expect(s).To(ContainSubstring("fact"))
		Expect(s).To(ContainSubstring("Some content"))
	})
})

var _ = Describe("MemorySearchResponse.MarshalJSON", func() {
	It("should marshal to valid JSON", func() {
		resp := vector.MemorySearchResponse{
			Results: []vector.MemorySearchResultItem{
				{Content: "test", Similarity: 0.9, Metadata: map[string]string{"type": "test"}},
			},
		}
		data, err := json.Marshal(resp)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).NotTo(BeEmpty())
		Expect(string(data)).To(ContainSubstring("test"))
	})
})

var _ = Describe("NewMemoryStoreTool", func() {
	It("should create a tool with the correct name", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, nil)
		Expect(t.Declaration().Name).To(Equal("memory_store"))
	})

	It("should store text via tool call", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, nil)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"text":"Remember this"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
	})
})

var _ = Describe("NewMemorySearchTool", func() {
	It("should create a tool with the correct name", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemorySearchTool(store, nil)
		Expect(t.Declaration().Name).To(Equal("memory_search"))
	})

	It("should search stored text via tool call", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		err = store.Add(context.Background(), vector.BatchItem{ID: "s1", Text: "Testing memory", Metadata: map[string]string{}})
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemorySearchTool(store, nil)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"query":"memory"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
	})
})

var _ = Describe("AllowedMetadataKeys", func() {
	It("memory_store rejects metadata keys not in allowed list when configured", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy", AllowedMetadataKeys: []string{"product", "category"}}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, &cfg)
		ct := t.(tool.CallableTool)

		_, err = ct.Call(context.Background(), []byte(`{"text":"x","metadata":{"product":"p1","unknown_key":"y"}}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown_key"))
	})

	It("memory_store accepts metadata when keys are in allowed list", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy", AllowedMetadataKeys: []string{"product", "category"}}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, &cfg)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"text":"fact","metadata":{"product":"AI SRE","category":"roadmap"}}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
	})

	It("memory_search with filter uses allowed keys and returns filtered results", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy", AllowedMetadataKeys: []string{"product"}}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		err = store.Add(context.Background(),
			vector.BatchItem{ID: "a", Text: "AI SRE feature", Metadata: map[string]string{"product": "ai-sre"}},
			vector.BatchItem{ID: "b", Text: "Other product note", Metadata: map[string]string{"product": "other"}},
		)
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemorySearchTool(store, &cfg)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"query":"feature","limit":5,"filter":{"product":"ai-sre"}}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(fmt.Sprint(result)).To(ContainSubstring("AI SRE feature"))
	})
})
