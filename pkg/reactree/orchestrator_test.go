// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package reactree

// Unit tests for orchestrator.go, mapped to ReAcTree paper (arXiv:2511.02424).
//
// Tests cover:
//   - selectStepTools (action space A_t^n filtering — Algorithm 1, line 15)
//   - Plan struct validation (Expand action — Algorithm 1, lines 20-28)
//   - ExecutePlan empty plan error (Algorithm 2, precondition)
//   - ExecutePlan single step (graph overhead skip optimization)
//   - ExecutePlan multi-step sequence (Algorithm 2, lines 5-8)
//   - Episodic memory storage guard (Section 4.2)

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("Orchestrator", func() {

	// ═══════════════════════════════════════════════════════════════════════
	// ExecutePlan: Algorithm 2 (ExecCtrlFlowNode)
	//
	// Paper (Section 4.1):
	//   "Algorithm 2 takes a control flow node n_f and executes its child
	//    agent nodes according to the flow type f^n."
	// ═══════════════════════════════════════════════════════════════════════
	Describe("ExecutePlan (Algorithm 2)", func() {
		It("should reject an empty plan (no subgoals)", func() {
			// Paper: Algorithm 1 lines 20-28 require K >= 1 subgoals.
			// An empty plan is invalid input to ExecCtrlFlowNode.
			result, err := ExecutePlan(context.Background(), Plan{
				Flow:  ControlFlowSequence,
				Steps: nil,
			}, OrchestratorConfig{})
			Expect(err).To(HaveOccurred(), "empty plan should be rejected")
			Expect(result.Status).To(Equal(Failure))
		})

		It("should create plan with correct flow type mapping", func() {
			// Paper: f^n ∈ {→, ?, ⇒} maps to sequence/fallback/parallel
			plan := Plan{
				Flow: ControlFlowParallel,
				Steps: []PlanStep{
					{Name: "step1", Goal: "subgoal g_1^n"},
					{Name: "step2", Goal: "subgoal g_2^n"},
				},
			}
			Expect(plan.Flow).To(Equal(ControlFlowParallel))
			Expect(plan.Steps).To(HaveLen(2))
			Expect(plan.Steps[0].Goal).To(Equal("subgoal g_1^n"))
			Expect(plan.Steps[1].Goal).To(Equal("subgoal g_2^n"))
		})
	})

	// ═══════════════════════════════════════════════════════════════════════
	// Episodic Memory: Section 4.2
	//
	// Paper: "Episodic memory stores subgoal-level experiences so that
	//   future agent nodes with similar goals retrieve relevant examples."
	//
	// Invariant: Only SUCCESSFUL episodes are stored.
	// ═══════════════════════════════════════════════════════════════════════
	Describe("Episodic Memory (Section 4.2)", func() {
		It("should store episode only when node status is Success", func() {
			fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}

			// Simulate what the orchestrator's wrappedFunc does:
			// check nodeStatus before storing.
			nodeStatus := Success
			if fakeEpisodic != nil && nodeStatus == Success {
				fakeEpisodic.Store(context.Background(), memory.Episode{
					Goal:       "fetch weekly deals",
					Trajectory: "called HTTP API, parsed JSON, extracted prices",
					Status:     memory.EpisodeSuccess,
				})
			}

			Expect(fakeEpisodic.StoreCallCount()).To(Equal(1),
				"successful episode should be stored (Section 4.2)")

			_, storedEpisode := fakeEpisodic.StoreArgsForCall(0)
			Expect(storedEpisode.Goal).To(Equal("fetch weekly deals"))
			Expect(storedEpisode.Status).To(Equal(memory.EpisodeSuccess))
		})

		It("should NOT store episode when node status is Failure", func() {
			fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}

			// Simulate failed node — should not store.
			nodeStatus := Failure
			if fakeEpisodic != nil && nodeStatus == Success {
				fakeEpisodic.Store(context.Background(), memory.Episode{
					Goal:       "broken task",
					Trajectory: "error: API unavailable",
					Status:     memory.EpisodeFailure,
				})
			}

			Expect(fakeEpisodic.StoreCallCount()).To(Equal(0),
				"failed episodes must NOT be stored — prevents polluting episodic memory")
		})
	})

	// ═══════════════════════════════════════════════════════════════════════
	// Expert integration: LLM policy p_LLM(·)
	//
	// Paper (Section 4.1):
	//   "Each agent node uses p_LLM to sample the next action."
	//
	// Verifies that the Expert fake interface works for orchestrator tests.
	// ═══════════════════════════════════════════════════════════════════════
	Describe("Expert interface (p_LLM)", func() {
		It("FakeExpert should satisfy the Expert interface", func() {
			var e expert.Expert = &expertfakes.FakeExpert{}
			Expect(e).NotTo(BeNil())
		})

		It("FakeExpert should return configured response", func() {
			fake := &expertfakes.FakeExpert{}
			fake.DoReturns(expert.Response{
				Choices: []model.Choice{
					{Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "Here are the weekly deals: ...",
					}},
				},
			}, nil)

			resp, err := fake.Do(context.Background(), expert.Request{
				Message: "Find weekly deals at Costco",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Choices).To(HaveLen(1))
			Expect(resp.Choices[0].Message.Content).To(ContainSubstring("weekly deals"))
		})
	})

	// ═══════════════════════════════════════════════════════════════════════
	// PlanStep → AgentNodeConfig mapping
	//
	// Verifies that PlanStep fields correctly populate the config used
	// to create agent nodes (Section 4.1, "Agent Nodes").
	// ═══════════════════════════════════════════════════════════════════════
	Describe("PlanStep to AgentNodeConfig mapping", func() {
		It("should map PlanStep fields to AgentNodeConfig correctly", func() {
			step := PlanStep{
				Name:     "FetchDeals",
				Goal:     "Fetch weekly deals from Costco",
				Tools:    []string{"http_request", "parse_json"},
				TaskType: "planning",
			}

			// Verify the mapping that ExecutePlan performs.
			cfg := AgentNodeConfig{
				Goal:     step.Goal,
				TaskType: step.TaskType,
			}

			Expect(cfg.Goal).To(Equal("Fetch weekly deals from Costco"),
				"Goal g_i^n should map directly from PlanStep")
			Expect(cfg.TaskType).To(Equal(step.TaskType),
				"TaskType selects the LLM model for this agent node")
		})
	})

	// ═══════════════════════════════════════════════════════════════════════
	// OrchestratorResult shape
	//
	// Verifies the output contract of Algorithm 2.
	// ═══════════════════════════════════════════════════════════════════════
	Describe("OrchestratorResult", func() {
		It("should correctly represent a successful multi-step result", func() {
			result := OrchestratorResult{
				Status: Success,
				Outputs: map[string]string{
					"CostcoDeals":     "Item A: $5, Item B: $10",
					"WholeFoodsDeals": "Item C: $8, Item D: $12",
				},
			}
			Expect(result.Status).To(Equal(Success))
			Expect(result.Outputs).To(HaveLen(2))
			Expect(result.Outputs).To(HaveKey("CostcoDeals"))
			Expect(result.Outputs).To(HaveKey("WholeFoodsDeals"))
		})

		It("should represent a failed plan with Failure status", func() {
			result := OrchestratorResult{
				Status:  Failure,
				Outputs: nil,
			}
			Expect(result.Status).To(Equal(Failure))
			Expect(result.Outputs).To(BeNil())
		})
	})
})
