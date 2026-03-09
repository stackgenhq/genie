package graph_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/graph"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// These tests invoke the graph tools through the CallableTool interface,
// exercising the unexported execute/neighbors/getEntity/shortestPath/
// Explore/assemble/expandDepth2/Execute(batch)/dispatch methods that
// were previously at 0% coverage.
var _ = Describe("graph tools via CallableTool", func() {
	var (
		ctx       context.Context
		store     *graph.InMemoryStore
		provider  *graph.ToolProvider
		storeTool tool.CallableTool
		queryTool tool.CallableTool
	)

	BeforeEach(func() {
		ctx = context.Background()
		store = newSeededStore(ctx)
		provider = graph.NewToolProvider(store)

		tools := provider.GetTools()
		Expect(tools).To(HaveLen(2))

		for _, t := range tools {
			ct, ok := t.(tool.CallableTool)
			Expect(ok).To(BeTrue(), "tool %s should implement CallableTool", t.Declaration().Name)
			if t.Declaration().Name == graph.GraphStoreToolName {
				storeTool = ct
			} else if t.Declaration().Name == graph.GraphQueryToolName {
				queryTool = ct
			}
		}
		Expect(storeTool).NotTo(BeNil())
		Expect(queryTool).NotTo(BeNil())
	})

	AfterEach(func() {
		if store != nil {
			_ = store.Close(ctx)
		}
	})

	// ---- graph_store tool ----

	Describe("graph_store execute", func() {
		It("stores an entity via action=entity", func() {
			input, _ := json.Marshal(graph.GraphStoreRequest{
				Action: "entity",
				ID:     "svc-api",
				Type:   "service",
				Attrs:  map[string]string{"port": "8080"},
			})

			result, err := storeTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Verify entity was stored
			entity, err := store.GetEntity(ctx, "svc-api")
			Expect(err).NotTo(HaveOccurred())
			Expect(entity).NotTo(BeNil())
			Expect(entity.Type).To(Equal("service"))
		})

		It("stores a relation via action=relation", func() {
			input, _ := json.Marshal(graph.GraphStoreRequest{
				Action:    "relation",
				SubjectID: "alice",
				Predicate: "CREATED",
				ObjectID:  "repo-1",
			})

			result, err := storeTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
		})

		It("returns error for action=entity with empty ID", func() {
			input, _ := json.Marshal(graph.GraphStoreRequest{
				Action: "entity",
				ID:     "",
				Type:   "person",
			})

			_, err := storeTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("id and type are required"))
		})

		It("returns error for action=entity with empty Type", func() {
			input, _ := json.Marshal(graph.GraphStoreRequest{
				Action: "entity",
				ID:     "x",
				Type:   "",
			})

			_, err := storeTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("id and type are required"))
		})

		It("returns error for action=relation with empty SubjectID", func() {
			input, _ := json.Marshal(graph.GraphStoreRequest{
				Action:    "relation",
				SubjectID: "",
				Predicate: "OWNS",
				ObjectID:  "b",
			})

			_, err := storeTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("subject_id, predicate, and object_id"))
		})

		It("returns error for unknown action", func() {
			input, _ := json.Marshal(graph.GraphStoreRequest{
				Action: "delete",
			})

			_, err := storeTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("action must be"))
		})
	})

	// ---- graph_query tool ----

	Describe("graph_query execute: action=neighbors", func() {
		It("returns neighbors for an existing entity", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "neighbors",
				EntityID: "alice",
				Limit:    20,
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
			Expect(resp.Count).To(Equal(2)) // bob, proj-x
			Expect(resp.Neighbors).To(HaveLen(2))
		})

		It("returns Found=false for non-existent entity", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "neighbors",
				EntityID: "ghost",
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeFalse())
		})

		It("returns error when entity_id is empty", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "neighbors",
				EntityID: "",
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("entity_id is required"))
		})

		It("uses default limit when limit <= 0", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "neighbors",
				EntityID: "alice",
				Limit:    0,
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Count).To(Equal(2)) // all alice's neighbors
		})
	})

	Describe("graph_query execute: action=get_entity", func() {
		It("returns existing entity", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "get_entity",
				EntityID: "bob",
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
			Expect(resp.Entity).NotTo(BeNil())
			Expect(resp.Entity.ID).To(Equal("bob"))
			Expect(resp.Entity.Type).To(Equal("person"))
		})

		It("returns Found=false for non-existent entity", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "get_entity",
				EntityID: "nonexistent",
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeFalse())
			Expect(resp.Entity).To(BeNil())
		})

		It("returns error when entity_id is empty", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "get_entity",
				EntityID: "",
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("entity_id is required"))
		})
	})

	Describe("graph_query execute: action=shortest_path", func() {
		It("finds path between connected entities", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "shortest_path",
				SourceID: "alice",
				TargetID: "repo-1",
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
			Expect(resp.Path).NotTo(BeEmpty())
			Expect(resp.Path[0]).To(Equal("alice"))
			Expect(resp.Path[len(resp.Path)-1]).To(Equal("repo-1"))
		})

		It("returns error when source or target is empty", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "shortest_path",
				SourceID: "",
				TargetID: "alice",
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("source_id and target_id are required"))
		})

		It("returns Found=false when no path exists", func() {
			// Add isolated entity
			Expect(store.AddEntity(ctx, graph.Entity{ID: "island", Type: "thing"})).To(Succeed())

			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "shortest_path",
				SourceID: "alice",
				TargetID: "island",
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeFalse())
		})
	})

	Describe("graph_query execute: action=explore", func() {
		It("returns full subgraph for alice at depth=1", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "explore",
				EntityID: "alice",
				Limit:    20,
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
			Expect(resp.Subgraph).NotTo(BeNil())
			Expect(resp.Subgraph.Root.ID).To(Equal("alice"))
			Expect(resp.Subgraph.Entities).To(HaveLen(2))  // bob, proj-x
			Expect(resp.Subgraph.Relations).To(HaveLen(2)) // WORKS_ON→proj-x, MENTORS→bob
			Expect(resp.Subgraph.Neighbors).To(HaveLen(2))
		})

		It("returns full subgraph for alice at depth=2", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "explore",
				EntityID: "alice",
				Limit:    20,
				Depth:    2,
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
			Expect(resp.Subgraph).NotTo(BeNil())
			Expect(resp.Subgraph.Root.ID).To(Equal("alice"))
			// Depth 2 should include repo-1 which is 2 hops away
			allEntityIDs := map[string]bool{resp.Subgraph.Root.ID: true}
			for _, e := range resp.Subgraph.Entities {
				allEntityIDs[e.ID] = true
			}
			Expect(allEntityIDs).To(HaveKey("repo-1"))
		})

		It("clamps depth > 2 to depth=2", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "explore",
				EntityID: "alice",
				Depth:    5, // should be clamped to 2
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
		})

		It("returns Found=false for non-existent entity", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "explore",
				EntityID: "nonexistent",
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeFalse())
		})

		It("returns error when entity_id is empty", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:   "explore",
				EntityID: "",
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("entity_id is required"))
		})
	})

	Describe("graph_query execute: action=batch", func() {
		It("runs multiple sub-queries in parallel", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action: "batch",
				Queries: []graph.BatchQuery{
					{Action: "get_entity", EntityID: "alice"},
					{Action: "neighbors", EntityID: "bob", Limit: 20},
					{Action: "shortest_path", SourceID: "alice", TargetID: "repo-1"},
				},
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.Found).To(BeTrue())
			Expect(resp.BatchResults).To(HaveLen(3))

			// Result 0: get_entity alice
			Expect(resp.BatchResults[0].Index).To(Equal(0))
			Expect(resp.BatchResults[0].Action).To(Equal("get_entity"))
			Expect(resp.BatchResults[0].Response).NotTo(BeNil())
			Expect(resp.BatchResults[0].Response.Found).To(BeTrue())
			Expect(resp.BatchResults[0].Response.Entity.ID).To(Equal("alice"))

			// Result 1: neighbors bob
			Expect(resp.BatchResults[1].Index).To(Equal(1))
			Expect(resp.BatchResults[1].Response).NotTo(BeNil())
			Expect(resp.BatchResults[1].Response.Neighbors).To(HaveLen(3))

			// Result 2: shortest_path alice → repo-1
			Expect(resp.BatchResults[2].Index).To(Equal(2))
			Expect(resp.BatchResults[2].Response).NotTo(BeNil())
			Expect(resp.BatchResults[2].Response.Path).NotTo(BeEmpty())
		})

		It("captures per-query errors in batch results", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action: "batch",
				Queries: []graph.BatchQuery{
					{Action: "get_entity", EntityID: "alice"},                // should succeed
					{Action: "get_entity", EntityID: ""},                     // should fail: empty entity_id
					{Action: "shortest_path", SourceID: "", TargetID: "bob"}, // should fail: empty source_id
				},
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred()) // batch-level error should NOT occur

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.BatchResults).To(HaveLen(3))

			// First query succeeds
			Expect(resp.BatchResults[0].Error).To(BeEmpty())
			Expect(resp.BatchResults[0].Response).NotTo(BeNil())

			// Second query fails with error
			Expect(resp.BatchResults[1].Error).NotTo(BeEmpty())
			Expect(resp.BatchResults[1].Response).To(BeNil())

			// Third query fails with error
			Expect(resp.BatchResults[2].Error).NotTo(BeEmpty())
		})

		It("returns error for empty queries array", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:  "batch",
				Queries: []graph.BatchQuery{},
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("queries array is required"))
		})

		It("returns error when batch size exceeds maximum", func() {
			queries := make([]graph.BatchQuery, 11)
			for i := range queries {
				queries[i] = graph.BatchQuery{Action: "get_entity", EntityID: "alice"}
			}
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action:  "batch",
				Queries: queries,
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("batch size"))
		})

		It("handles unsupported batch sub-action", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action: "batch",
				Queries: []graph.BatchQuery{
					{Action: "unsupported_action", EntityID: "alice"},
				},
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.BatchResults).To(HaveLen(1))
			Expect(resp.BatchResults[0].Error).To(ContainSubstring("unsupported batch sub-action"))
		})

		It("supports explore in batch", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action: "batch",
				Queries: []graph.BatchQuery{
					{Action: "explore", EntityID: "alice", Limit: 20, Depth: 1},
					{Action: "explore", EntityID: "bob", Limit: 20, Depth: 1},
				},
			})

			result, err := queryTool.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resp, ok := result.(graph.GraphQueryResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.BatchResults).To(HaveLen(2))

			// Both should succeed
			for _, br := range resp.BatchResults {
				Expect(br.Error).To(BeEmpty())
				Expect(br.Response).NotTo(BeNil())
				Expect(br.Response.Subgraph).NotTo(BeNil())
			}
		})
	})

	Describe("graph_query execute: invalid action", func() {
		It("returns error for unknown action", func() {
			input, _ := json.Marshal(graph.GraphQueryRequest{
				Action: "delete_all",
			})

			_, err := queryTool.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("action must be"))
		})
	})
})
