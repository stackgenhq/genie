// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"context"
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/memory/graph"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/datetime"
	"github.com/stackgenhq/genie/pkg/tools/math"
)

var _ = Describe("VectorToolProvider", func() {
	var (
		store vector.IStore
	)

	BeforeEach(func(ctx context.Context) {
		var err error
		cfg := vector.Config{
			VectorStoreProvider: "inmemory",
			EmbeddingProvider:   "dummy",
		}
		store, err = cfg.NewStore(ctx)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("NewVectorToolProvider", func() {
		It("returns error when vector store is nil", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			_, err := tools.NewVectorToolProvider(ctx, nil, reg, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("vector store is nil"))
		})

		It("indexes tools from the registry", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(vtp).NotTo(BeNil())
		})

		It("succeeds with an empty registry", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(vtp).NotTo(BeNil())
		})

		It("returns error when upsert fails during indexing", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(fmt.Errorf("upsert exploded"))

			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			_, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("upsert"))
		})
	})

	Describe("SearchTools", func() {
		It("returns relevant tools for a query", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "calculate math", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())

			for _, r := range results {
				Expect(r.Name).NotTo(BeEmpty())
				Expect(r.Description).NotTo(BeEmpty())
			}
		})

		It("returns results with scores", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "calculator", 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())
			for _, r := range results {
				Expect(r.Score).To(BeNumerically(">=", 0))
			}
		})

		It("returns error when search fails", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns(nil, fmt.Errorf("search broke"))

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			_, err = vtp.SearchTools(ctx, "anything", 5)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("search"))
		})

		It("skips results with empty tool_name metadata", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{
				{
					Content:  "some_tool: does things",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "some_tool"},
					Score:    0.9,
				},
				{
					Content:  "orphan entry without tool_name",
					Metadata: map[string]string{"type": "tool_index"},
					Score:    0.8,
				},
				{
					Content:  "empty name entry",
					Metadata: map[string]string{"type": "tool_index", "tool_name": ""},
					Score:    0.7,
				},
			}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "tools", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Name).To(Equal("some_tool"))
		})

		It("extracts description by splitting on ': ' separator", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{
				{
					Content:  "my_tool: Does amazing things",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "my_tool"},
					Score:    0.9,
				},
			}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "amazing", 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Description).To(Equal("Does amazing things"))
		})

		It("uses full content as description when no separator exists", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{
				{
					Content:  "no separator here",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "weird_tool"},
					Score:    0.8,
				},
			}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "weird", 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Description).To(Equal("no separator here"))
		})

		It("respects limit parameter", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "tools", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(results)).To(BeNumerically("<=", 1))
		})

		It("uses default limit when given zero", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "math", 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeNil())
		})

		It("uses default limit when given negative value", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "math", -5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeNil())
		})
	})

	Describe("RecordToolUsage", func() {
		It("records pairwise co-occurrence for multiple tools", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"read_file", "write_file", "run_shell"})

			Expect(vtp.CooccurrenceScore("read_file", "write_file")).To(BeNumerically(">", 0))
			Expect(vtp.CooccurrenceScore("read_file", "run_shell")).To(BeNumerically(">", 0))
			Expect(vtp.CooccurrenceScore("write_file", "run_shell")).To(BeNumerically(">", 0))
		})

		It("is symmetric", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"tool_a", "tool_b"})

			Expect(vtp.CooccurrenceScore("tool_a", "tool_b")).To(
				Equal(vtp.CooccurrenceScore("tool_b", "tool_a")))
		})

		It("ignores single-tool usage", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"only_tool"})

			Expect(vtp.CooccurrenceScore("only_tool", "anything")).To(Equal(0.0))
		})

		It("ignores empty tool list", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{})
			// Should not panic.
		})

		It("accumulates weights over multiple recordings", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"a", "b"})
			score1 := vtp.CooccurrenceScore("a", "b")

			vtp.RecordToolUsage(ctx, []string{"a", "b"})
			score2 := vtp.CooccurrenceScore("a", "b")

			// After the first recording, a-b count=1 and max=1, so score=1.0.
			// After the second, count=2 and max=2, so score is still 1.0.
			// But if we add another pairing, the original pair should dominate.
			vtp.RecordToolUsage(ctx, []string{"a", "c"})
			scoreAB := vtp.CooccurrenceScore("a", "b")
			scoreAC := vtp.CooccurrenceScore("a", "c")

			// a-b (count=2) should score higher than a-c (count=1)
			Expect(scoreAB).To(BeNumerically(">", scoreAC))
			// Both initial scores should be positive.
			Expect(score1).To(BeNumerically(">", 0))
			Expect(score2).To(BeNumerically(">", 0))
		})
	})

	Describe("CooccurrenceScore", func() {
		It("returns 0 for unknown tools", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(vtp.CooccurrenceScore("unknown_a", "unknown_b")).To(Equal(0.0))
		})

		It("returns 0 when graph is empty", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(vtp.CooccurrenceScore("a", "b")).To(Equal(0.0))
		})

		It("returns 1.0 for the max edge weight pair", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"a", "b"})
			// Only pair, so it's the max edge.
			Expect(vtp.CooccurrenceScore("a", "b")).To(Equal(1.0))
		})

		It("returns 0 for tools that exist but have no edge", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"a", "b"})
			vtp.RecordToolUsage(ctx, []string{"c", "d"})

			Expect(vtp.CooccurrenceScore("a", "d")).To(Equal(0.0))
		})

		It("uses log normalization for diminishing returns", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			// Record a-b 10 times and c-d once.
			for i := 0; i < 10; i++ {
				vtp.RecordToolUsage(ctx, []string{"a", "b"})
			}
			vtp.RecordToolUsage(ctx, []string{"c", "d"})

			scoreAB := vtp.CooccurrenceScore("a", "b")
			scoreCD := vtp.CooccurrenceScore("c", "d")

			// Max is a-b at 10, c-d is 1.
			// log(1+1)/log(1+10) ≈ 0.289 (not 0.1 — log normalisation).
			Expect(scoreAB).To(Equal(1.0))
			Expect(scoreCD).To(BeNumerically(">", 0.2))
			Expect(scoreCD).To(BeNumerically("<", 0.4))
		})
	})

	Describe("SearchToolsWithContext", func() {
		It("returns pure semantic results when graph is empty (cold start)", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchToolsWithContext(ctx, "calculate", []string{"run_shell"}, 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())
		})

		It("returns pure semantic results when no context tools given", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"calculator", "run_shell"})

			results, err := vtp.SearchToolsWithContext(ctx, "calculate", nil, 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())
		})

		It("boosts tools with co-occurrence affinity", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{
				{
					Content:  "tool_a: Does A things",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_a"},
					Score:    0.5, // Lower semantic score
				},
				{
					Content:  "tool_b: Does B things",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_b"},
					Score:    0.6, // Higher semantic score
				},
			}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			// Record that tool_a is often used with context_tool
			for i := 0; i < 5; i++ {
				vtp.RecordToolUsage(ctx, []string{"tool_a", "context_tool"})
			}

			results, err := vtp.SearchToolsWithContext(ctx, "things", []string{"context_tool"}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))

			// tool_a should be ranked first because:
			// tool_a blended = 0.7*0.5 + 0.3*1.0 = 0.65
			// tool_b blended = 0.7*0.6 + 0.3*0.0 = 0.42
			Expect(results[0].Name).To(Equal("tool_a"))
			Expect(results[1].Name).To(Equal("tool_b"))
		})

		It("respects limit after re-ranking", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{
				{
					Content:  "tool_1: First",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_1"},
					Score:    0.9,
				},
				{
					Content:  "tool_2: Second",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_2"},
					Score:    0.8,
				},
				{
					Content:  "tool_3: Third",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_3"},
					Score:    0.7,
				},
			}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"tool_1", "ctx"})

			results, err := vtp.SearchToolsWithContext(ctx, "tools", []string{"ctx"}, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
		})

		It("returns error when search fails", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns(nil, fmt.Errorf("search error"))

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"a", "b"})

			_, err = vtp.SearchToolsWithContext(ctx, "anything", []string{"a"}, 5)
			Expect(err).To(HaveOccurred())
		})

		It("uses default limit when given zero", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchToolsWithContext(ctx, "math", []string{}, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeNil())
		})
	})

	Describe("FormatToolList", func() {
		It("formats results as markdown-style list", func() {
			results := tools.ToolSearchResults{
				{Name: "calculator", Description: "Evaluate math expressions"},
				{Name: "datetime", Description: "Get current date and time"},
			}
			formatted := results.String()
			Expect(formatted).To(ContainSubstring("- calculator: Evaluate math expressions"))
			Expect(formatted).To(ContainSubstring("- datetime: Get current date and time"))
		})

		It("returns empty string for empty results", func() {
			formatted := tools.ToolSearchResults{}.String()
			Expect(formatted).To(BeEmpty())
		})

		It("returns empty string for empty slice", func() {
			formatted := tools.ToolSearchResults{}.String()
			Expect(formatted).To(BeEmpty())
		})

		It("handles single result", func() {
			formatted := tools.ToolSearchResults{
				{Name: "only_tool", Description: "The only tool"},
			}.String()
			Expect(formatted).To(Equal("- only_tool: The only tool\n"))
		})
	})

	Describe("Idempotent re-indexing", func() {
		It("re-indexing with same registry does not duplicate", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())

			vtp1, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results1, err := vtp1.SearchTools(ctx, "calculator", 20)
			Expect(err).NotTo(HaveOccurred())

			vtp2, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results2, err := vtp2.SearchTools(ctx, "calculator", 20)
			Expect(err).NotTo(HaveOccurred())

			countToolResults := func(results []tools.ToolSearchResult) int {
				count := 0
				for _, r := range results {
					if r.Name == "calculator" {
						count++
					}
				}
				return count
			}

			Expect(countToolResults(results2)).To(Equal(countToolResults(results1)))
		})
	})

	Describe("Thread safety", func() {
		It("handles concurrent RecordToolUsage calls", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			var wg sync.WaitGroup
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					vtp.RecordToolUsage(ctx, []string{
						fmt.Sprintf("tool_%d", i%5),
						fmt.Sprintf("tool_%d", (i+1)%5),
					})
				}(i)
			}
			wg.Wait()

			// Should not panic and scores should be positive for recorded pairs.
			score := vtp.CooccurrenceScore("tool_0", "tool_1")
			Expect(score).To(BeNumerically(">", 0))
		})

		It("handles concurrent reads and writes", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			var wg sync.WaitGroup
			// Writers
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					vtp.RecordToolUsage(ctx, []string{"read_file", "write_file"})
				}(i)
			}
			// Readers
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = vtp.CooccurrenceScore("read_file", "write_file")
				}()
			}
			wg.Wait()
			// No race condition = pass.
		})
	})

	Describe("Edge cases with fake store", func() {
		It("returns empty results when store search returns empty", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "anything", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("handles multiple tools with same name gracefully", func(ctx context.Context) {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{
				{
					Content:  "tool_a: First version",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_a"},
					Score:    0.9,
				},
				{
					Content:  "tool_a: Second version",
					Metadata: map[string]string{"type": "tool_index", "tool_name": "tool_a"},
					Score:    0.8,
				},
			}, nil)

			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "tool_a", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
		})
	})

	Describe("Graph store persistence", func() {
		It("persists co-occurrence to graph store and restores on new instance", func(ctx context.Context) {
			graphStore, gErr := graph.NewInMemoryStore()
			Expect(gErr).NotTo(HaveOccurred())

			reg := tools.NewRegistry(ctx)

			// First instance: record tool usage.
			vtp1, err := tools.NewVectorToolProvider(ctx, store, reg, graphStore)
			Expect(err).NotTo(HaveOccurred())

			vtp1.RecordToolUsage(ctx, []string{"read_file", "write_file", "run_shell"})

			originalScore := vtp1.CooccurrenceScore("read_file", "write_file")
			Expect(originalScore).To(BeNumerically(">", 0))

			// Second instance: should restore from graph store.
			vtp2, err := tools.NewVectorToolProvider(ctx, store, reg, graphStore)
			Expect(err).NotTo(HaveOccurred())

			restoredScore := vtp2.CooccurrenceScore("read_file", "write_file")
			Expect(restoredScore).To(Equal(originalScore))

			// All pairs should be restored.
			Expect(vtp2.CooccurrenceScore("read_file", "run_shell")).To(BeNumerically(">", 0))
			Expect(vtp2.CooccurrenceScore("write_file", "run_shell")).To(BeNumerically(">", 0))
		})

		It("accumulates across instances", func(ctx context.Context) {
			graphStore, gErr := graph.NewInMemoryStore()
			Expect(gErr).NotTo(HaveOccurred())

			reg := tools.NewRegistry(ctx)

			vtp1, err := tools.NewVectorToolProvider(ctx, store, reg, graphStore)
			Expect(err).NotTo(HaveOccurred())
			vtp1.RecordToolUsage(ctx, []string{"a", "b"})

			vtp2, err := tools.NewVectorToolProvider(ctx, store, reg, graphStore)
			Expect(err).NotTo(HaveOccurred())
			vtp2.RecordToolUsage(ctx, []string{"a", "b"})
			vtp2.RecordToolUsage(ctx, []string{"a", "c"})

			// a-b (count=2) should score higher than a-c (count=1)
			Expect(vtp2.CooccurrenceScore("a", "b")).To(BeNumerically(">", vtp2.CooccurrenceScore("a", "c")))
		})

		It("works without graph store (nil, ephemeral mode)", func(ctx context.Context) {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, nil)
			Expect(err).NotTo(HaveOccurred())

			vtp.RecordToolUsage(ctx, []string{"a", "b"})
			Expect(vtp.CooccurrenceScore("a", "b")).To(BeNumerically(">", 0))
		})
	})

	Describe("DisableCooccurrenceCache", func() {
		It("reads scores directly from graph store when cache is disabled", func(ctx context.Context) {
			graphStore, gErr := graph.NewInMemoryStore()
			Expect(gErr).NotTo(HaveOccurred())

			reg := tools.NewRegistry(ctx)

			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, graphStore)
			Expect(err).NotTo(HaveOccurred())
			vtp.DisableCooccurrenceCache = true

			vtp.RecordToolUsage(ctx, []string{"tool_x", "tool_y"})

			// Should still work — reads from graph store directly.
			score := vtp.CooccurrenceScore("tool_x", "tool_y")
			Expect(score).To(BeNumerically(">", 0))
			Expect(score).To(Equal(1.0)) // Only pair, so max.
		})

		It("returns 0 for unknown tools when cache is disabled", func(ctx context.Context) {
			graphStore, gErr := graph.NewInMemoryStore()
			Expect(gErr).NotTo(HaveOccurred())

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg, graphStore)
			Expect(err).NotTo(HaveOccurred())
			vtp.DisableCooccurrenceCache = true

			Expect(vtp.CooccurrenceScore("unknown", "other")).To(Equal(0.0))
		})
	})
})
