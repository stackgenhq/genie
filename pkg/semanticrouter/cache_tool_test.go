// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticrouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/semanticrouter"
	"github.com/stackgenhq/genie/pkg/semanticrouter/semanticrouterfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("cacheTool", func() {
	var (
		fakeRouter *semanticrouterfakes.FakeIRouter
		cacheTool  tool.Tool
	)

	BeforeEach(func() {
		fakeRouter = &semanticrouterfakes.FakeIRouter{}
		cacheTool = semanticrouter.NewCacheTool(fakeRouter)
		Expect(cacheTool).NotTo(BeNil())
	})

	// callTool is a test helper that marshals req to JSON, calls the tool, and
	// unmarshals the result into a CacheToolResponse.
	callTool := func(ctx context.Context, req semanticrouter.CacheToolRequest) (semanticrouter.CacheToolResponse, error) {
		reqJSON, err := json.Marshal(req)
		Expect(err).NotTo(HaveOccurred())

		callable, ok := cacheTool.(tool.CallableTool)
		Expect(ok).To(BeTrue(), "tool should implement CallableTool")

		result, err := callable.Call(ctx, reqJSON)
		if err != nil {
			return semanticrouter.CacheToolResponse{}, err
		}

		// The function tool returns the struct directly; marshal then unmarshal
		// to get a clean CacheToolResponse.
		resultJSON, marshalErr := json.Marshal(result)
		Expect(marshalErr).NotTo(HaveOccurred())

		var resp semanticrouter.CacheToolResponse
		Expect(json.Unmarshal(resultJSON, &resp)).To(Succeed())
		return resp, nil
	}

	Describe("search", func() {
		It("should return matching cache entries", func(ctx context.Context) {
			fakeRouter.SearchCacheReturns([]semanticrouter.CacheEntry{
				{ID: "cache_abc", Query: "deploy app", Response: "deployed", Score: 0.95, CachedAt: time.Now()},
				{ID: "cache_def", Query: "pod status", Response: "all healthy", Score: 0.88, CachedAt: time.Now()},
			}, nil)

			resp, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "deploy",
				Limit:  10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(2))
			Expect(resp.Entries).To(HaveLen(2))
			Expect(resp.Entries[0].ID).To(Equal("cache_abc"))

			Expect(fakeRouter.SearchCacheCallCount()).To(Equal(1))
			_, query, limit := fakeRouter.SearchCacheArgsForCall(0)
			Expect(query).To(Equal("deploy"))
			Expect(limit).To(Equal(10))
		})

		It("should require query for search", func(ctx context.Context) {
			_, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query is required"))
		})

		It("should default limit to 20", func(ctx context.Context) {
			fakeRouter.SearchCacheReturns(nil, nil)

			_, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "test",
			})
			Expect(err).NotTo(HaveOccurred())

			_, _, limit := fakeRouter.SearchCacheArgsForCall(0)
			Expect(limit).To(Equal(20))
		})

		It("should cap limit at 50", func(ctx context.Context) {
			fakeRouter.SearchCacheReturns(nil, nil)

			_, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "test",
				Limit:  100,
			})
			Expect(err).NotTo(HaveOccurred())

			_, _, limit := fakeRouter.SearchCacheArgsForCall(0)
			Expect(limit).To(Equal(50))
		})

		It("should propagate search errors", func(ctx context.Context) {
			fakeRouter.SearchCacheReturns(nil, errors.New("search failed"))

			_, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("search failed"))
		})
	})

	Describe("delete", func() {
		It("should delete entries by IDs", func(ctx context.Context) {
			fakeRouter.DeleteCacheEntriesReturns(3, nil)

			resp, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    []string{"id1", "id2", "id3"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(3))
			Expect(resp.Message).To(ContainSubstring("Deleted 3"))

			Expect(fakeRouter.DeleteCacheEntriesCallCount()).To(Equal(1))
			_, ids := fakeRouter.DeleteCacheEntriesArgsForCall(0)
			Expect(ids).To(ConsistOf("id1", "id2", "id3"))
		})

		It("should require IDs for delete", func(ctx context.Context) {
			_, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    nil,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ids array is required"))
		})

		It("should propagate delete errors", func(ctx context.Context) {
			fakeRouter.DeleteCacheEntriesReturns(0, errors.New("delete failed"))

			_, err := callTool(ctx, semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    []string{"id1"},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("clear_all", func() {
		It("should clear all cache entries", func(ctx context.Context) {
			fakeRouter.ClearCacheReturns(4, nil)

			resp, err := callTool(ctx, semanticrouter.CacheToolRequest{Action: "clear_all"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(4))
			Expect(resp.Message).To(ContainSubstring("Cleared all 4"))

			Expect(fakeRouter.ClearCacheCallCount()).To(Equal(1))
		})

		It("should handle empty cache gracefully", func(ctx context.Context) {
			fakeRouter.ClearCacheReturns(0, nil)

			resp, err := callTool(ctx, semanticrouter.CacheToolRequest{Action: "clear_all"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(0))
		})
	})

	Describe("invalid action", func() {
		It("should return error for unknown action", func(ctx context.Context) {
			_, err := callTool(ctx, semanticrouter.CacheToolRequest{Action: "unknown"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown"))
		})
	})

	Describe("NewCacheTool", func() {
		It("should return nil when router is nil", func() {
			t := semanticrouter.NewCacheTool(nil)
			Expect(t).To(BeNil())
		})

		It("should return a tool with correct name", func() {
			Expect(cacheTool.Declaration().Name).To(Equal(semanticrouter.CacheToolName))
		})
	})
})
