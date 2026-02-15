package reactree_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/reactree"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/reactree/memory/memoryfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("TreeExecutor (Adaptive Loop)", func() {
	var (
		fakeExpert   *expertfakes.FakeExpert
		fakeEpisodic *memoryfakes.FakeEpisodicMemory
		treeExec     reactree.TreeExecutor
		ctx          context.Context
		config       reactree.TreeConfig
	)

	BeforeEach(func() {
		fakeExpert = &expertfakes.FakeExpert{}
		fakeEpisodic = &memoryfakes.FakeEpisodicMemory{}
		ctx = context.Background()

		config = reactree.DefaultTreeConfig()
		// Default to adaptive loop mode
		config.MaxIterations = 5
		config.Stages = nil

		treeExec = reactree.NewTreeExecutor(
			fakeExpert,
			memory.NewWorkingMemory(),
			fakeEpisodic,
			config,
		)
	})

	Context("when running in adaptive loop mode", func() {
		It("should finish in 1 iteration if task is completed immediately", func() {
			// Mock expert to return a response with zero tool calls (TaskCompleted=true)
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Task done immediately.",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Simple task",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
			Expect(result.NodeCount).To(Equal(1))
			Expect(result.Output).To(Equal("Task done immediately."))
			Expect(fakeExpert.DoCallCount()).To(Equal(1))
		})

		It("should run multiple iterations and accumulate context", func() {
			// Iteration 1: performed some work (tool call simulates work), returns output
			// Iteration 2: finishes the task (no tool calls)

			// We need to simulate tool calls to prevent early exit in the first iteration.
			// The current logic checks toolCallCount == 0 to set TaskCompleted.

			// Mocking Do for multiple calls
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Working on step 1...",
							ToolCalls: []model.ToolCall{
								{}, // Presence of tool call implies work is ongoing
							},
						},
					},
				},
			}, nil)

			fakeExpert.DoReturnsOnCall(1, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Task completed.",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Complex task",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
			Expect(result.NodeCount).To(Equal(2))
			Expect(result.Output).To(Equal("Task completed."))
			Expect(fakeExpert.DoCallCount()).To(Equal(2))

			// Verify context accumulation in 2nd call
			_, argReq := fakeExpert.DoArgsForCall(1)
			Expect(argReq.Message).To(ContainSubstring("## Progress So Far"))
			Expect(argReq.Message).To(ContainSubstring("Working on step 1..."))
		})

		It("should stop when MaxIterations is reached", func() {
			config.MaxIterations = 3
			treeExec = reactree.NewTreeExecutor(
				fakeExpert,
				memory.NewWorkingMemory(),
				fakeEpisodic,
				config,
			)

			// Always return tool calls so it never naturally completes
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Still working...",
							ToolCalls: []model.ToolCall{
								{}, // Always simulate tool usage
							},
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Never ending task",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// Status might be Success because the last node execution was successful (even if task not "completed")
			// The loop just stops. logic says "lastStatus" is returned.
			// If the last node executed successfully (expert returned success), status is Success.
			// However, since it terminated by reaching max iterations (Condition 3 in runAdaptiveLoop),
			// it returns the status of the last iteration, which is Success.
			Expect(result.Status).To(Equal(reactree.Success))
			Expect(result.NodeCount).To(Equal(3))
		})

		It("should stop immediately on error", func() {
			fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("API error"))

			req := reactree.TreeRequest{
				Goal: "Error task",
			}

			// NewAgentNodeFunc catches the error and returns Output="error: ...", Status=Failure.
			// The adaptive loop sees Failure and breaks.

			result, err := treeExec.Run(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Failure))
			Expect(result.Output).To(ContainSubstring("API error"))
			Expect(fakeExpert.DoCallCount()).To(Equal(1))
		})
	})

	Context("Context Window Truncation", func() {
		It("should truncate large accumulated context", func() {
			// Iteration 1: Large output
			largeOutput := strings.Repeat("A", 5000)
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: largeOutput,
							ToolCalls: []model.ToolCall{
								{}, // tool used
							},
						},
					},
				},
			}, nil)

			// Iteration 2: Finish
			fakeExpert.DoReturnsOnCall(1, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Done",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Large context task",
			}

			_, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// check 2nd call prompt
			_, argReq := fakeExpert.DoArgsForCall(1)
			Expect(argReq.Message).To(ContainSubstring("... (earlier output truncated)"))
			// Should contain the END of the large output
			Expect(argReq.Message).To(ContainSubstring(strings.Repeat("A", 100)))
		})
	})

	Context("Legacy Multi-Stage Fallback", func() {
		BeforeEach(func() {
			config.MaxIterations = 0 // Disable adaptive loop
			config.Stages = []reactree.StageConfig{
				{Name: "Stage1"},
				{Name: "Stage2"},
			}
			treeExec = reactree.NewTreeExecutor(
				fakeExpert,
				memory.NewWorkingMemory(),
				fakeEpisodic,
				config,
			)
		})

		It("should run stages if configured", func() {
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Stage done",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{Goal: "Staged task"}
			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))

			// Because of EarlyExit, if Stage1 completes (no tool calls), Stage2 is skipped.
			// Mock returns no tool calls, so it should finish after Stage 1.
			// runMultiStage returns NodeCount = configured stages (2), not executed stages.
			// So we check DoCallCount to verify early exit.
			Expect(fakeExpert.DoCallCount()).To(Equal(1))
		})
	})
})
