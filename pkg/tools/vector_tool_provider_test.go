// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/datetime"
	"github.com/stackgenhq/genie/pkg/tools/math"
)

var _ = Describe("VectorToolProvider", func() {
	var (
		ctx   context.Context
		store vector.IStore
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cfg := vector.Config{
			VectorStoreProvider: "inmemory",
			EmbeddingProvider:   "dummy",
		}
		store, err = cfg.NewStore(ctx)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("NewVectorToolProvider", func() {
		It("returns error when vector store is nil", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			_, err := tools.NewVectorToolProvider(ctx, nil, reg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("vector store is nil"))
		})

		It("indexes tools from the registry", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())
			Expect(vtp).NotTo(BeNil())
		})

		It("succeeds with an empty registry", func() {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())
			Expect(vtp).NotTo(BeNil())
		})

		It("returns error when upsert fails during indexing", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(fmt.Errorf("upsert exploded"))

			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			_, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("upsert"))
		})
	})

	Describe("SearchTools", func() {
		It("returns relevant tools for a query", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "calculate math", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())

			// All results should have non-empty names.
			for _, r := range results {
				Expect(r.Name).NotTo(BeEmpty())
				Expect(r.Description).NotTo(BeEmpty())
			}
		})

		It("returns results with scores", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "calculator", 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeEmpty())
			// Dummy embedder scores are deterministic.
			for _, r := range results {
				Expect(r.Score).To(BeNumerically(">=", 0))
			}
		})

		It("returns error when search fails", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			// First: upsert succeeds (indexing).
			fakeStore.UpsertReturns(nil)
			// Second: search fails.
			fakeStore.SearchReturns(nil, fmt.Errorf("search broke"))

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).NotTo(HaveOccurred())

			_, err = vtp.SearchTools(ctx, "anything", 5)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("search"))
		})

		It("skips results with empty tool_name metadata", func() {
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
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "tools", 10)
			Expect(err).NotTo(HaveOccurred())
			// Only the entry with a non-empty tool_name should be returned.
			Expect(results).To(HaveLen(1))
			Expect(results[0].Name).To(Equal("some_tool"))
		})

		It("extracts description by splitting on ': ' separator", func() {
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
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "amazing", 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Description).To(Equal("Does amazing things"))
		})

		It("uses full content as description when no separator exists", func() {
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
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "weird", 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Description).To(Equal("no separator here"))
		})

		It("respects limit parameter", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "tools", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(results)).To(BeNumerically("<=", 1))
		})

		It("uses default limit when given zero", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "math", 0)
			Expect(err).NotTo(HaveOccurred())
			// Should not panic; default limit of 10 is used.
			Expect(results).NotTo(BeNil())
		})

		It("uses default limit when given negative value", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "math", -5)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).NotTo(BeNil())
		})
	})

	Describe("FormatToolList", func() {
		It("formats results as markdown-style list", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results := []tools.ToolSearchResult{
				{Name: "calculator", Description: "Evaluate math expressions"},
				{Name: "datetime", Description: "Get current date and time"},
			}
			formatted := vtp.FormatToolList(results)
			Expect(formatted).To(ContainSubstring("- calculator: Evaluate math expressions"))
			Expect(formatted).To(ContainSubstring("- datetime: Get current date and time"))
		})

		It("returns empty string for empty results", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			formatted := vtp.FormatToolList(nil)
			Expect(formatted).To(BeEmpty())
		})

		It("returns empty string for empty slice", func() {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			formatted := vtp.FormatToolList([]tools.ToolSearchResult{})
			Expect(formatted).To(BeEmpty())
		})

		It("handles single result", func() {
			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			formatted := vtp.FormatToolList([]tools.ToolSearchResult{
				{Name: "only_tool", Description: "The only tool"},
			})
			Expect(formatted).To(Equal("- only_tool: The only tool\n"))
		})
	})

	Describe("Idempotent re-indexing", func() {
		It("re-indexing with same registry does not duplicate", func() {
			reg := tools.NewRegistry(ctx, math.NewToolProvider())

			// Index first time
			vtp1, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results1, err := vtp1.SearchTools(ctx, "calculator", 20)
			Expect(err).NotTo(HaveOccurred())

			// Index again (upsert should overwrite)
			vtp2, err := tools.NewVectorToolProvider(ctx, store, reg)
			Expect(err).NotTo(HaveOccurred())

			results2, err := vtp2.SearchTools(ctx, "calculator", 20)
			Expect(err).NotTo(HaveOccurred())

			// Filter to only tool_index results (by name match).
			// Count should be same or fewer, never more.
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

	Describe("Edge cases with fake store", func() {
		It("returns empty results when store search returns empty", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.UpsertReturns(nil)
			fakeStore.SearchReturns([]vector.SearchResult{}, nil)

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "anything", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("handles multiple tools with same name gracefully", func() {
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

			reg := tools.NewRegistry(ctx)
			vtp, err := tools.NewVectorToolProvider(ctx, fakeStore, reg)
			Expect(err).NotTo(HaveOccurred())

			results, err := vtp.SearchTools(ctx, "tool_a", 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].Name).To(Equal("tool_a"))
			Expect(results[1].Name).To(Equal("tool_a"))
		})
	})
})
