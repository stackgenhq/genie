// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/vector"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// These tests verify that memory_search and memory_list tools filter out
// graph documents (tagged with __graph_type metadata) from their results.
var _ = Describe("Graph document filtering in memory tools", func() {
	var (
		ctx   context.Context
		store vector.IStore
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		cfg := vector.Config{EmbeddingProvider: "dummy"}
		store, err = cfg.NewStore(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Seed the store with a mix of regular memory docs AND graph docs.
		err = store.Add(ctx, vector.AddRequest{Items: []vector.BatchItem{
			{ID: "mem1", Text: "User prefers dark mode", Metadata: map[string]string{"source": "chat"}},
			{ID: "mem2", Text: "User works on kubernetes", Metadata: map[string]string{"source": "chat"}},
			{
				ID:       "graph:entity:alice",
				Text:     `{"id":"alice","type":"person"}`,
				Metadata: map[string]string{"__graph_type": "entity", "graph_entity_id": "alice"},
			},
			{
				ID:       "graph:relation:alice:KNOWS:bob",
				Text:     `{"subject_id":"alice","predicate":"KNOWS","object_id":"bob"}`,
				Metadata: map[string]string{"__graph_type": "relation", "graph_subject_id": "alice"},
			},
		}})
		Expect(err).NotTo(HaveOccurred())
	})

	callTool := func(t tool.Tool, reqJSON []byte) (map[string]interface{}, error) {
		callable, ok := t.(tool.CallableTool)
		Expect(ok).To(BeTrue())

		result, err := callable.Call(ctx, reqJSON)
		if err != nil {
			return nil, err
		}

		resultJSON, marshalErr := json.Marshal(result)
		Expect(marshalErr).NotTo(HaveOccurred())

		var resp map[string]interface{}
		Expect(json.Unmarshal(resultJSON, &resp)).To(Succeed())
		return resp, nil
	}

	Describe("memory_search", func() {
		It("should not return graph entity/relation docs", func() {
			searchTool := vector.NewMemorySearchTool(store, nil)
			reqJSON, _ := json.Marshal(map[string]interface{}{
				"query": "alice person entity",
				"limit": 10,
			})

			resp, err := callTool(searchTool, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			results, ok := resp["results"].([]interface{})
			Expect(ok).To(BeTrue())

			// Only regular memory docs should appear, not graph docs.
			for _, r := range results {
				item := r.(map[string]interface{})
				meta, hasMeta := item["metadata"].(map[string]interface{})
				if hasMeta {
					Expect(meta).NotTo(HaveKey("__graph_type"),
						"graph docs should be filtered out of memory_search results")
				}
			}
		})

		It("should still return regular memory documents", func() {
			searchTool := vector.NewMemorySearchTool(store, nil)
			reqJSON, _ := json.Marshal(map[string]interface{}{
				"query": "dark mode",
				"limit": 10,
			})

			resp, err := callTool(searchTool, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			results, ok := resp["results"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(results).NotTo(BeEmpty(), "regular memory docs should still be returned")
		})
	})

	Describe("memory_list", func() {
		It("should not return graph docs in list results", func() {
			listTool := vector.NewMemoryListTool(store, nil)
			reqJSON, _ := json.Marshal(map[string]interface{}{
				"limit": 20,
			})

			resp, err := callTool(listTool, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			entries, ok := resp["entries"].([]interface{})
			if ok {
				for _, e := range entries {
					item := e.(map[string]interface{})
					meta, hasMeta := item["metadata"].(map[string]interface{})
					if hasMeta {
						Expect(meta).NotTo(HaveKey("__graph_type"),
							"graph docs should be filtered out of memory_list results")
					}
				}
			}
		})
	})
})
