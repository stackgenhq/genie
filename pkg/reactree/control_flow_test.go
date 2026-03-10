// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package reactree_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/reactree"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// stubNodeFunc creates a graph.NodeFunc that returns a fixed NodeStatus.
func stubNodeFunc(status reactree.NodeStatus) graph.NodeFunc {
	return func(_ context.Context, _ graph.State) (any, error) {
		return graph.State{
			reactree.StateKeyNodeStatus: status,
		}, nil
	}
}

// stubNodeFuncWithCompletion creates a graph.NodeFunc that returns a fixed
// NodeStatus and TaskCompleted flag. Used to test early-exit behavior.
func stubNodeFuncWithCompletion(status reactree.NodeStatus, completed bool) graph.NodeFunc {
	return func(_ context.Context, _ graph.State) (any, error) {
		return graph.State{
			reactree.StateKeyNodeStatus:    status,
			reactree.StateKeyTaskCompleted: completed,
		}, nil
	}
}

// trackingNodeFunc creates a graph.NodeFunc that sets a flag when executed.
// Used to verify that a node was (or was not) reached during graph execution.
func trackingNodeFunc(status reactree.NodeStatus, executed *bool) graph.NodeFunc {
	return func(_ context.Context, _ graph.State) (any, error) {
		*executed = true
		return graph.State{
			reactree.StateKeyNodeStatus: status,
		}, nil
	}
}

// testInvocation creates a minimal agent.Invocation for test execution.
func testInvocation() *agent.Invocation {
	return agent.NewInvocation()
}

var _ = Describe("ControlFlow", func() {

	Describe("BuildSequence", func() {
		It("should compile a valid sequence graph with all-success nodes", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			sg.AddNode("a", stubNodeFunc(reactree.Success))
			sg.AddNode("b", stubNodeFunc(reactree.Success))
			sg.SetEntryPoint("a")

			reactree.BuildSequence(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())
			Expect(compiled).NotTo(BeNil())
		})

		It("should compile and execute a sequence where first node fails (short-circuits)", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			sg.AddNode("a", stubNodeFunc(reactree.Failure))
			sg.AddNode("b", stubNodeFunc(reactree.Success))
			sg.SetEntryPoint("a")

			reactree.BuildSequence(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())

			executor, err := graph.NewExecutor(compiled)
			Expect(err).NotTo(HaveOccurred())

			events, err := executor.Execute(context.Background(), graph.State{}, testInvocation())
			Expect(err).NotTo(HaveOccurred())

			// Drain events — just verify no panic or error
			for range events {
			}
		})

		It("should handle empty node list gracefully", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)
			result := reactree.BuildSequence(sg, []string{})
			Expect(result).NotTo(BeNil())
		})
	})

	Describe("BuildSequenceWithEarlyExit", func() {
		It("should skip remaining stages when task is completed (zero tool calls)", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			nodeBExecuted := false
			sg.AddNode("a", stubNodeFuncWithCompletion(reactree.Success, true)) // completed = true → early exit
			sg.AddNode("b", trackingNodeFunc(reactree.Success, &nodeBExecuted))
			sg.SetEntryPoint("a")

			reactree.BuildSequenceWithEarlyExit(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())

			executor, err := graph.NewExecutor(compiled)
			Expect(err).NotTo(HaveOccurred())

			events, err := executor.Execute(context.Background(), graph.State{}, testInvocation())
			Expect(err).NotTo(HaveOccurred())
			for range events {
			}

			// Node b should NOT have been executed because a signaled completion
			Expect(nodeBExecuted).To(BeFalse(), "node b should be skipped when task completed early")
		})

		It("should continue to next stage when task is not completed", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			nodeBExecuted := false
			sg.AddNode("a", stubNodeFuncWithCompletion(reactree.Success, false)) // completed = false → continue
			sg.AddNode("b", trackingNodeFunc(reactree.Success, &nodeBExecuted))
			sg.SetEntryPoint("a")

			reactree.BuildSequenceWithEarlyExit(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())

			executor, err := graph.NewExecutor(compiled)
			Expect(err).NotTo(HaveOccurred())

			events, err := executor.Execute(context.Background(), graph.State{}, testInvocation())
			Expect(err).NotTo(HaveOccurred())
			for range events {
			}

			// Node b SHOULD have been executed because task was not completed
			Expect(nodeBExecuted).To(BeTrue(), "node b should execute when task is not completed")
		})

		It("should still short-circuit on failure", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			nodeBExecuted := false
			sg.AddNode("a", stubNodeFuncWithCompletion(reactree.Failure, false))
			sg.AddNode("b", trackingNodeFunc(reactree.Success, &nodeBExecuted))
			sg.SetEntryPoint("a")

			reactree.BuildSequenceWithEarlyExit(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())

			executor, err := graph.NewExecutor(compiled)
			Expect(err).NotTo(HaveOccurred())

			events, err := executor.Execute(context.Background(), graph.State{}, testInvocation())
			Expect(err).NotTo(HaveOccurred())
			for range events {
			}

			Expect(nodeBExecuted).To(BeFalse(), "node b should be skipped on failure")
		})

		It("should handle empty node list gracefully", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)
			result := reactree.BuildSequenceWithEarlyExit(sg, []string{})
			Expect(result).NotTo(BeNil())
		})
	})

	Describe("BuildFallback", func() {
		It("should compile a valid fallback graph", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			sg.AddNode("a", stubNodeFunc(reactree.Failure))
			sg.AddNode("b", stubNodeFunc(reactree.Success))
			sg.SetEntryPoint("a")

			reactree.BuildFallback(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())
			Expect(compiled).NotTo(BeNil())
		})

		It("should compile and execute fallback where all fail", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			sg.AddNode("a", stubNodeFunc(reactree.Failure))
			sg.AddNode("b", stubNodeFunc(reactree.Failure))
			sg.SetEntryPoint("a")

			reactree.BuildFallback(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())

			executor, err := graph.NewExecutor(compiled)
			Expect(err).NotTo(HaveOccurred())

			events, err := executor.Execute(context.Background(), graph.State{}, testInvocation())
			Expect(err).NotTo(HaveOccurred())

			for range events {
			}
		})

		It("should handle empty node list gracefully", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)
			result := reactree.BuildFallback(sg, []string{})
			Expect(result).NotTo(BeNil())
		})
	})

	Describe("BuildParallel", func() {
		It("should compile a valid parallel graph with aggregator", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			// Wrap nodes for parallel so per-node status keys are written for majority voting.
			sg.AddNode("a", reactree.WrapNodeForParallel("a", stubNodeFunc(reactree.Success)))
			sg.AddNode("b", reactree.WrapNodeForParallel("b", stubNodeFunc(reactree.Failure)))
			sg.AddNode("c", reactree.WrapNodeForParallel("c", stubNodeFunc(reactree.Success)))

			// Fan out from start to all nodes
			sg.SetEntryPoint("a")
			sg.AddEdge(graph.Start, "b")
			sg.AddEdge(graph.Start, "c")

			reactree.BuildParallel(sg, []string{"a", "b", "c"}, "aggregator")

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())
			Expect(compiled).NotTo(BeNil())
		})

		It("should handle empty node list gracefully", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)
			result := reactree.BuildParallel(sg, []string{}, "agg")
			Expect(result).NotTo(BeNil())
		})
	})

	Describe("statusRouter (via Sequence integration)", func() {
		It("should route to failure branch when node fails", func() {
			schema := reactree.NewReAcTreeSchema()
			sg := graph.NewStateGraph(schema)

			sg.AddNode("a", stubNodeFunc(reactree.Failure))
			sg.AddNode("b", stubNodeFunc(reactree.Success))
			sg.SetEntryPoint("a")

			reactree.BuildSequence(sg, []string{"a", "b"})

			compiled, err := sg.Compile()
			Expect(err).NotTo(HaveOccurred())

			executor, err := graph.NewExecutor(compiled)
			Expect(err).NotTo(HaveOccurred())

			events, err := executor.Execute(context.Background(), graph.State{}, testInvocation())
			Expect(err).NotTo(HaveOccurred())

			// Graph should complete without errors
			for range events {
			}
		})
	})

	Describe("NewReAcTreeSchema", func() {
		It("should create a valid schema with all required fields", func() {
			schema := reactree.NewReAcTreeSchema()
			Expect(schema).NotTo(BeNil())
			Expect(schema.Fields).To(HaveKey(reactree.StateKeyGoal))
			Expect(schema.Fields).To(HaveKey(reactree.StateKeyNodeStatus))
			Expect(schema.Fields).To(HaveKey(reactree.StateKeyOutput))
			Expect(schema.Fields).To(HaveKey(reactree.StateKeyWorkingMemory))
			Expect(schema.Fields).To(HaveKey(reactree.StateKeyTaskCompleted))
		})
	})

	Describe("NodeStatus", func() {
		It("should have correct string representations", func() {
			Expect(reactree.Running.String()).To(Equal("running"))
			Expect(reactree.Success.String()).To(Equal("success"))
			Expect(reactree.Failure.String()).To(Equal("failure"))
		})
	})
})
