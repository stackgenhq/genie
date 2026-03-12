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

	"github.com/stackgenhq/genie/pkg/identity"
	"github.com/stackgenhq/genie/pkg/rbac"
	"github.com/stackgenhq/genie/pkg/semanticrouter"
	"github.com/stackgenhq/genie/pkg/semanticrouter/semanticrouterfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("cacheTool", func() {
	var (
		fakeRouter *semanticrouterfakes.FakeIRouter
		cacheTool  tool.Tool
		testRBAC   *rbac.RBAC
	)

	BeforeEach(func() {
		fakeRouter = &semanticrouterfakes.FakeIRouter{}
		testRBAC = rbac.New(rbac.Config{AdminUsers: []string{"admin@co.com"}})
		cacheTool = semanticrouter.NewCacheTool(fakeRouter, testRBAC)
		Expect(cacheTool).NotTo(BeNil())
	})

	// adminCtx returns a context with an admin sender.
	adminCtx := func() context.Context {
		return identity.WithSender(context.Background(), identity.Sender{
			ID:   "admin@co.com",
			Role: "user",
		})
	}

	// regularCtx returns a context with a non-admin user.
	regularCtx := func() context.Context {
		return identity.WithSender(context.Background(), identity.Sender{
			ID:   "regular@co.com",
			Role: "user",
		})
	}

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

		resultJSON, marshalErr := json.Marshal(result)
		Expect(marshalErr).NotTo(HaveOccurred())

		var resp semanticrouter.CacheToolResponse
		Expect(json.Unmarshal(resultJSON, &resp)).To(Succeed())
		return resp, nil
	}

	Describe("search", func() {
		It("should return matching cache entries", func() {
			ctx := regularCtx() // search is open to all
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

		It("should require query for search", func() {
			_, err := callTool(regularCtx(), semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("query is required"))
		})

		It("should default limit to 20", func() {
			fakeRouter.SearchCacheReturns(nil, nil)

			_, err := callTool(regularCtx(), semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "test",
			})
			Expect(err).NotTo(HaveOccurred())

			_, _, limit := fakeRouter.SearchCacheArgsForCall(0)
			Expect(limit).To(Equal(20))
		})

		It("should cap limit at 50", func() {
			fakeRouter.SearchCacheReturns(nil, nil)

			_, err := callTool(regularCtx(), semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "test",
				Limit:  100,
			})
			Expect(err).NotTo(HaveOccurred())

			_, _, limit := fakeRouter.SearchCacheArgsForCall(0)
			Expect(limit).To(Equal(50))
		})

		It("should propagate search errors", func() {
			fakeRouter.SearchCacheReturns(nil, errors.New("search failed"))

			_, err := callTool(regularCtx(), semanticrouter.CacheToolRequest{
				Action: "search",
				Query:  "test",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("search failed"))
		})
	})

	Describe("delete", func() {
		It("should delete entries by IDs when admin", func() {
			fakeRouter.DeleteCacheEntriesReturns(3, nil)

			resp, err := callTool(adminCtx(), semanticrouter.CacheToolRequest{
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

		It("should deny delete for non-admin users", func() {
			_, err := callTool(regularCtx(), semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    []string{"id1"},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("permission denied"))
			Expect(fakeRouter.DeleteCacheEntriesCallCount()).To(Equal(0))
		})

		It("should allow demo user (no auth)", func() {
			// context.Background() → GetSender returns DemoSender (role="demo")
			fakeRouter.DeleteCacheEntriesReturns(1, nil)

			resp, err := callTool(context.Background(), semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    []string{"id1"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(1))
		})

		It("should require IDs for delete", func() {
			_, err := callTool(adminCtx(), semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    nil,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ids array is required"))
		})

		It("should propagate delete errors", func() {
			fakeRouter.DeleteCacheEntriesReturns(0, errors.New("delete failed"))

			_, err := callTool(adminCtx(), semanticrouter.CacheToolRequest{
				Action: "delete",
				IDs:    []string{"id1"},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("clear_all", func() {
		It("should clear all cache entries when admin", func() {
			fakeRouter.ClearCacheReturns(4, nil)

			resp, err := callTool(adminCtx(), semanticrouter.CacheToolRequest{Action: "clear_all"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(4))
			Expect(resp.Message).To(ContainSubstring("Cleared all 4"))

			Expect(fakeRouter.ClearCacheCallCount()).To(Equal(1))
		})

		It("should deny clear_all for non-admin users", func() {
			_, err := callTool(regularCtx(), semanticrouter.CacheToolRequest{Action: "clear_all"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("permission denied"))
			Expect(fakeRouter.ClearCacheCallCount()).To(Equal(0))
		})

		It("should handle empty cache gracefully", func() {
			fakeRouter.ClearCacheReturns(0, nil)

			resp, err := callTool(adminCtx(), semanticrouter.CacheToolRequest{Action: "clear_all"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(0))
		})
	})

	Describe("invalid action", func() {
		It("should return error for unknown action", func() {
			_, err := callTool(adminCtx(), semanticrouter.CacheToolRequest{Action: "unknown"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown"))
		})
	})

	Describe("NewCacheTool", func() {
		It("should return nil when router is nil", func() {
			t := semanticrouter.NewCacheTool(nil, nil)
			Expect(t).To(BeNil())
		})

		It("should return a tool with correct name", func() {
			Expect(cacheTool.Declaration().Name).To(Equal(semanticrouter.CacheToolName))
		})
	})
})
