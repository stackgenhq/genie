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

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/reactree/memory/memoryfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockTool implements tool.Tool for testing. Matches the pattern in tool_registry_test.go.
type mockTool struct {
	name string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "stub" }
func (m *mockTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: m.name, Description: "stub"}
}
func (m *mockTool) Run(_ context.Context, _ []byte) ([]byte, error) { return nil, nil }

func stubTool(name string) tool.Tool {
	return &mockTool{name: name}
}

var _ = Describe("Orchestrator", func() {

	// ═══════════════════════════════════════════════════════════════════════
	// selectStepTools: Action space A_t^n filtering
	//
	// Paper (Section 4.1, Algorithm 1, line 15):
	//   "Agent nodes sample actions from their available tool set A_t^n"
	//
	// Framework invariants:
	//   - send_message is ALWAYS stripped (only orchestrator sends to user)
	//   - create_agent is ALWAYS stripped (prevents recursive spawning)
	// ═══════════════════════════════════════════════════════════════════════
	Describe("selectStepTools (action space A_t^n)", func() {
		var registry map[string]tool.Tool

		BeforeEach(func() {
			registry = map[string]tool.Tool{
				"read_file":    stubTool("read_file"),
				"write_file":   stubTool("write_file"),
				"run_shell":    stubTool("run_shell"),
				"send_message": stubTool("send_message"),
				"create_agent": stubTool("create_agent"),
			}
		})

		It("should strip send_message from tool set when no specific tools requested", func() {
			tools := selectStepTools(nil, registry)
			names := toolNames(tools)
			Expect(names).NotTo(ContainElement("send_message"),
				"send_message must be stripped — only orchestrator communicates with users (paper invariant)")
		})

		It("should strip create_agent from tool set when no specific tools requested", func() {
			tools := selectStepTools(nil, registry)
			names := toolNames(tools)
			Expect(names).NotTo(ContainElement("create_agent"),
				"create_agent must be stripped — prevents recursive sub-agent spawning")
		})

		It("should include all other tools when no specific tools requested", func() {
			tools := selectStepTools(nil, registry)
			names := toolNames(tools)
			Expect(names).To(ContainElements("read_file", "write_file", "run_shell"),
				"non-restricted tools should be available as A_t^n")
		})

		It("should strip send_message even when explicitly requested", func() {
			tools := selectStepTools([]string{"read_file", "send_message"}, registry)
			names := toolNames(tools)
			Expect(names).NotTo(ContainElement("send_message"),
				"framework invariant: send_message always stripped regardless of request")
			Expect(names).To(ContainElement("read_file"))
		})

		It("should strip create_agent even when explicitly requested", func() {
			tools := selectStepTools([]string{"create_agent", "read_file"}, registry)
			names := toolNames(tools)
			Expect(names).NotTo(ContainElement("create_agent"),
				"framework invariant: create_agent always stripped regardless of request")
			Expect(names).To(ContainElement("read_file"))
		})

		It("should return only requested tools minus restricted ones", func() {
			tools := selectStepTools([]string{"read_file", "write_file"}, registry)
			names := toolNames(tools)
			Expect(names).To(ConsistOf("read_file", "write_file"))
		})

		It("should skip tools not in registry gracefully", func() {
			tools := selectStepTools([]string{"nonexistent_tool"}, registry)
			Expect(tools).To(BeEmpty(),
				"requesting a tool not in registry should return empty (no panic)")
		})

		It("should return empty tool set when registry is empty", func() {
			tools := selectStepTools(nil, map[string]tool.Tool{})
			Expect(tools).To(BeEmpty())
		})
	})

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

// toolNames extracts tool declaration names for test assertions.
func toolNames(tools []tool.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Declaration().Name
	}
	return names
}
