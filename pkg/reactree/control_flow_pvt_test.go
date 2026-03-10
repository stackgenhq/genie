// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package reactree

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

var _ = Describe("Control Flow", func() {
	Describe("contains (case-insensitive substring)", func() {
		DescribeTable("returns expected result",
			func(s, substr string, expected bool) {
				Expect(contains(s, substr)).To(Equal(expected))
			},
			Entry("exact match", "HALT", "HALT", true),
			Entry("case-insensitive", "please halt now", "HALT", true),
			Entry("uppercase in lowercase", "PROCEED", "proceed", true),
			Entry("mixed case both", "hAlT", "HaLt", true),
			Entry("partial match at end", "...HALT", "HALT", true),
			Entry("empty substr", "anything", "", true),
			Entry("not found", "continue", "HALT", false),
			Entry("empty string", "", "HALT", false),
		)
	})

	Describe("statusRouter", func() {
		DescribeTable("routes based on NodeStatus",
			func(status interface{}, expected string) {
				state := graph.State{}
				if status != nil {
					state[StateKeyNodeStatus] = status
				}
				result, err := statusRouter(context.Background(), state)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(expected))
			},
			Entry("success", Success, "success"),
			Entry("failure", Failure, "failure"),
			Entry("running → failure", Running, "failure"),
			Entry("missing status → failure", nil, "failure"),
		)
	})

	Describe("stageRouter", func() {
		It("returns completed when task is done", func() {
			state := graph.State{
				StateKeyNodeStatus:    Success,
				StateKeyTaskCompleted: true,
			}
			result, err := stageRouter(context.Background(), state)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("completed"))
		})

		It("returns success when task is not completed", func() {
			state := graph.State{StateKeyNodeStatus: Success}
			result, err := stageRouter(context.Background(), state)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("success"))
		})
	})

	Describe("majorityVoteFunc", func() {
		DescribeTable("votes correctly",
			func(nodeIDs []string, statuses map[string]NodeStatus, expected NodeStatus) {
				state := graph.State{}
				for id, s := range statuses {
					state[StateKeyNodeStatus+":"+id] = s
				}
				fn := majorityVoteFunc(nodeIDs)
				result, err := fn(context.Background(), state)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.(graph.State)[StateKeyNodeStatus]).To(Equal(expected))
			},
			Entry("majority success", []string{"a", "b", "c"},
				map[string]NodeStatus{"a": Success, "b": Success, "c": Failure}, Success),
			Entry("majority failure", []string{"a", "b", "c"},
				map[string]NodeStatus{"a": Failure, "b": Failure, "c": Success}, Failure),
			Entry("tie → failure", []string{"a", "b"},
				map[string]NodeStatus{"a": Success, "b": Failure}, Failure),
			Entry("single success", []string{"a"},
				map[string]NodeStatus{"a": Success}, Success),
			Entry("no status keys → failure", []string{"a", "b"},
				map[string]NodeStatus{}, Failure),
		)
	})

	Describe("WrapNodeForParallel", func() {
		It("copies NodeStatus to per-node key", func() {
			original := func(_ context.Context, _ graph.State) (any, error) {
				return graph.State{StateKeyNodeStatus: Success, "output": "done"}, nil
			}
			wrapped := WrapNodeForParallel("myNode", original)
			result, err := wrapped(context.Background(), graph.State{})
			Expect(err).NotTo(HaveOccurred())
			s := result.(graph.State)
			Expect(s[StateKeyNodeStatus]).To(Equal(Success))
			Expect(s[StateKeyNodeStatus+":myNode"]).To(Equal(Success))
		})

		It("preserves error from original", func() {
			// Arrange
			original := func(_ context.Context, _ graph.State) (any, error) {
				return nil, fmt.Errorf("node failed")
			}

			// Act
			_, err := WrapNodeForParallel("errNode", original)(context.Background(), graph.State{})

			// Assert
			Expect(err).To(MatchError("node failed"))
		})

		It("handles non-State result", func() {
			original := func(_ context.Context, _ graph.State) (any, error) { return "plain", nil }
			result, err := WrapNodeForParallel("p", original)(context.Background(), graph.State{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("plain"))
		})

		It("handles State without NodeStatus", func() {
			original := func(_ context.Context, _ graph.State) (any, error) {
				return graph.State{"output": "data"}, nil
			}
			result, err := WrapNodeForParallel("ns", original)(context.Background(), graph.State{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.(graph.State)).NotTo(HaveKey(StateKeyNodeStatus + ":ns"))
		})
	})

	Describe("NodeStatus.String", func() {
		DescribeTable("returns correct string",
			func(s NodeStatus, expected string) {
				Expect(s.String()).To(ContainSubstring(expected))
			},
			Entry("running", Running, "running"),
			Entry("success", Success, "success"),
			Entry("failure", Failure, "failure"),
			Entry("unknown", NodeStatus(99), "unknown"),
		)
	})
})
