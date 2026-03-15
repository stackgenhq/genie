// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/qdrantstore"
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
			err = store.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{{ID: "1", Text: "The sky is blue", Metadata: map[string]string{"type": "fact"}}}})
			Expect(err).NotTo(HaveOccurred())

			err = store.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{{ID: "2", Text: "Apples are red", Metadata: map[string]string{"type": "fact"}}}})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, vector.SearchRequest{Query: "The sky is blue", Limit: 1})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].ID).To(Equal("1"))
			Expect(results[0].Content).To(Equal("The sky is blue"))
			Expect(results[0].Score).To(BeNumerically(">", 0))
		})

		It("should delete documents by ID", func() {
			err = store.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{{ID: "del-1", Text: "To be deleted", Metadata: map[string]string{"type": "temp"}}}})
			Expect(err).NotTo(HaveOccurred())

			err = store.Delete(ctx, vector.DeleteRequest{IDs: []string{"del-1"}})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, vector.SearchRequest{Query: "To be deleted", Limit: 1})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should upsert by replacing existing document with same ID", func() {
			err = store.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{{ID: "upsert-1", Text: "Original content", Metadata: map[string]string{"v": "1"}}}})
			Expect(err).NotTo(HaveOccurred())

			err = store.Upsert(ctx, vector.UpsertRequest{Items: []vector.BatchItem{{ID: "upsert-1", Text: "Updated content", Metadata: map[string]string{"v": "2"}}}})
			Expect(err).NotTo(HaveOccurred())

			results, err := store.Search(ctx, vector.SearchRequest{Query: "Updated content", Limit: 1})
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

			err = store1.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{{ID: "persist-1", Text: "Go is great", Metadata: map[string]string{"lang": "go"}}}})
			Expect(err).NotTo(HaveOccurred())

			// Create store 2 from the same directory — should restore.
			store2, err := cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())

			results, err := store2.Search(ctx, vector.SearchRequest{Query: "Go is great", Limit: 1})
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

			err = s1.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{{ID: "del-test", Text: "deletable", Metadata: map[string]string{}}}})
			Expect(err).NotTo(HaveOccurred())

			err = s1.Delete(ctx, vector.DeleteRequest{IDs: []string{"del-test"}})
			Expect(err).NotTo(HaveOccurred())

			err = s1.Close(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Reload — deleted item should be gone
			s2, err := cfg.NewStore(ctx)
			Expect(err).NotTo(HaveOccurred())
			results, err := s2.Search(ctx, vector.SearchRequest{Query: "deletable", Limit: 1})
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

		t := vector.NewMemoryStoreTool(store, nil, nil)
		Expect(t.Declaration().Name).To(Equal("memory_store"))
	})

	It("should store text via tool call", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, nil, nil)
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

		err = store.Add(context.Background(), vector.AddRequest{Items: []vector.BatchItem{{ID: "s1", Text: "Testing memory", Metadata: map[string]string{}}}})
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemorySearchTool(store, nil)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"query":"memory"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
	})

	It("should return an empty result and set empty-retrieval span tag when no memories match", func(ctx context.Context) {
		// Arrange: store is empty so any query yields 0 results.
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Set up a recording tracer so we can inspect span attributes.
		original := otel.GetTracerProvider()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
		)
		otel.SetTracerProvider(tp)
		DeferCleanup(func() { otel.SetTracerProvider(original) })

		ctx, span := tp.Tracer("test").Start(ctx, "test-span")
		defer span.End()

		// Act
		t := vector.NewMemorySearchTool(store, nil)
		ct := t.(tool.CallableTool)
		result, err := ct.Call(ctx, []byte(`{"query":"something not stored"}`))

		// Assert: no error and 0 results
		Expect(err).NotTo(HaveOccurred())
		// The response Count field should be 0 (empty results).
		resp, ok := result.(vector.MemorySearchResponse)
		Expect(ok).To(BeTrue(), "expected MemorySearchResponse type")
		Expect(resp.Count).To(Equal(0))

		// Assert: the "empty-retrieval" tag was set on the active span.
		span.End()
		spans := exporter.GetSpans()
		Expect(spans).NotTo(BeEmpty())
		lastSpan := spans[len(spans)-1]
		var foundEmptyRetrieval bool
		for _, attr := range lastSpan.Attributes {
			if string(attr.Key) == "langfuse.trace.tags" {
				for _, v := range attr.Value.AsStringSlice() {
					if v == "empty-retrieval" {
						foundEmptyRetrieval = true
					}
				}
			}
		}
		Expect(foundEmptyRetrieval).To(BeTrue(), "expected 'empty-retrieval' tag on span")
	})
})

var _ = Describe("NewMemoryDeleteTool", func() {
	It("should create a tool with the correct name", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryDeleteTool(store)
		Expect(t.Declaration().Name).To(Equal("memory_delete"))
	})

	It("should delete a stored entry by ID", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		err = store.Add(context.Background(), vector.AddRequest{Items: []vector.BatchItem{{ID: "del-tool-1", Text: "stale error data", Metadata: map[string]string{}}}})
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryDeleteTool(store)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"ids":["del-tool-1"]}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(fmt.Sprint(result)).To(ContainSubstring("Successfully deleted 1"))

		// Verify the entry is gone.
		results, err := store.Search(context.Background(), vector.SearchRequest{Query: "stale error data", Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeEmpty())
	})

	It("should reject empty IDs", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryDeleteTool(store)
		ct := t.(tool.CallableTool)

		_, err = ct.Call(context.Background(), []byte(`{"ids":[]}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one ID"))
	})
})

var _ = Describe("NewMemoryListTool", func() {
	It("should create a tool with the correct name", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryListTool(store, nil)
		Expect(t.Declaration().Name).To(Equal("memory_list"))
	})

	It("should list stored entries", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		err = store.Add(context.Background(), vector.AddRequest{Items: []vector.BatchItem{
			{ID: "list-1", Text: "first memory", Metadata: map[string]string{"type": "test"}},
			{ID: "list-2", Text: "second memory", Metadata: map[string]string{"type": "test"}},
		}})
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryListTool(store, nil)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"limit": 10}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
	})
})

var _ = Describe("NewMemoryMergeTool", func() {
	It("should create a tool with the correct name", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryMergeTool(store, nil)
		Expect(t.Declaration().Name).To(Equal("memory_merge"))
	})

	It("should merge two entries into one and delete the second", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		err = store.Add(context.Background(), vector.AddRequest{Items: []vector.BatchItem{
			{ID: "merge-1", Text: "part one", Metadata: map[string]string{}},
			{ID: "merge-2", Text: "part two", Metadata: map[string]string{}},
		}})
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryMergeTool(store, nil)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"ids":["merge-1","merge-2"],"merged_text":"combined part one and two"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(fmt.Sprint(result)).To(ContainSubstring("Successfully merged 2"))

		// Verify merge-1 has the new content.
		results, err := store.Search(context.Background(), vector.SearchRequest{Query: "combined part one and two", Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(HaveLen(1))
		Expect(results[0].ID).To(Equal("merge-1"))
		Expect(results[0].Content).To(Equal("combined part one and two"))

		// Verify merge-2 is deleted.
		results, err = store.Search(context.Background(), vector.SearchRequest{Query: "part two", Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		for _, r := range results {
			Expect(r.ID).NotTo(Equal("merge-2"))
		}
	})

	It("should reject fewer than 2 IDs", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryMergeTool(store, nil)
		ct := t.(tool.CallableTool)

		_, err = ct.Call(context.Background(), []byte(`{"ids":["only-one"],"merged_text":"text"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least 2 IDs"))
	})
})

var _ = Describe("AllowedMetadataKeys", func() {
	It("memory_store rejects metadata keys not in allowed list when configured", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy", AllowedMetadataKeys: []string{"product", "category"}}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, &cfg, nil)
		ct := t.(tool.CallableTool)

		_, err = ct.Call(context.Background(), []byte(`{"text":"x","metadata":{"product":"p1","unknown_key":"y"}}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown_key"))
	})

	It("memory_store accepts metadata when keys are in allowed list", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy", AllowedMetadataKeys: []string{"product", "category"}}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemoryStoreTool(store, &cfg, nil)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"text":"fact","metadata":{"product":"AI SRE","category":"roadmap"}}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
	})

	It("memory_search with filter uses allowed keys and returns filtered results", func() {
		cfg := vector.Config{EmbeddingProvider: "dummy", AllowedMetadataKeys: []string{"product"}}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())

		err = store.Add(context.Background(), vector.AddRequest{Items: []vector.BatchItem{
			{ID: "a", Text: "AI SRE feature", Metadata: map[string]string{"product": "ai-sre"}},
			{ID: "b", Text: "Other product note", Metadata: map[string]string{"product": "other"}},
		}})
		Expect(err).NotTo(HaveOccurred())

		t := vector.NewMemorySearchTool(store, &cfg)
		ct := t.(tool.CallableTool)

		result, err := ct.Call(context.Background(), []byte(`{"query":"feature","limit":5,"filter":{"product":"ai-sre"}}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(fmt.Sprint(result)).To(ContainSubstring("AI SRE feature"))
	})
})

var _ = Describe("NewStore provider validation", func() {
	It("rejects an invalid vector_store_provider", func() {
		cfg := vector.Config{
			EmbeddingProvider:   "dummy",
			VectorStoreProvider: "invalid_provider",
		}
		_, err := cfg.NewStore(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid vector_store_provider"))
		Expect(err.Error()).To(ContainSubstring("invalid_provider"))
	})

	It("accepts explicit inmemory provider", func() {
		cfg := vector.Config{
			EmbeddingProvider:   "dummy",
			VectorStoreProvider: "inmemory",
		}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(store).NotTo(BeNil())
	})

	It("handles case-insensitive and whitespace-trimmed provider names", func() {
		cfg := vector.Config{
			EmbeddingProvider:   "dummy",
			VectorStoreProvider: "  InMemory  ",
		}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(store).NotTo(BeNil())
	})

	It("defaults to inmemory when provider is empty", func() {
		cfg := vector.Config{
			EmbeddingProvider:   "dummy",
			VectorStoreProvider: "",
		}
		store, err := cfg.NewStore(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(store).NotTo(BeNil())
	})
})

var _ = Describe("QdrantConfig", func() {
	It("has sensible zero-value defaults", func() {
		cfg := qdrantstore.Config{}
		Expect(cfg.Host).To(BeEmpty())
		Expect(cfg.Port).To(Equal(0))
		Expect(cfg.APIKey).To(BeEmpty())
		Expect(cfg.UseTLS).To(BeFalse())
		Expect(cfg.CollectionName).To(BeEmpty())
		Expect(cfg.Dimension).To(Equal(0))
	})

	It("round-trips non-default values", func() {
		cfg := qdrantstore.Config{
			Host:           "qdrant.example.com",
			Port:           6334,
			APIKey:         "test-key",
			UseTLS:         true,
			CollectionName: "my_collection",
			Dimension:      768,
		}
		Expect(cfg.Host).To(Equal("qdrant.example.com"))
		Expect(cfg.Port).To(Equal(6334))
		Expect(cfg.APIKey).To(Equal("test-key"))
		Expect(cfg.UseTLS).To(BeTrue())
		Expect(cfg.CollectionName).To(Equal("my_collection"))
		Expect(cfg.Dimension).To(Equal(768))
	})
})
