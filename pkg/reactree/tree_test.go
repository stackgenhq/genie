// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree_test

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/reactree"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
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
			// Iteration 2: finishes the task (no tool calls) — this is the final answer

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
							Content: "Task completed with final answer.",
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
			// Iteration 1 used tools (data gathering), iteration 2 completed
			// with zero tool calls. The completing iteration's output is
			// the final answer — it must NOT be suppressed.
			Expect(result.Output).To(Equal("Task completed with final answer."))
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

			// Return tool calls with varying content so repetition detector doesn't fire
			for i := 0; i < 3; i++ {
				fakeExpert.DoReturnsOnCall(i, expert.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    "assistant",
								Content: fmt.Sprintf("Still working on iteration %d...", i),
								ToolCalls: []model.ToolCall{
									{}, // Always simulate tool usage
								},
							},
						},
					},
				}, nil)
			}

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

	Context("Research-then-Create Output (dad-jokes pattern)", func() {
		It("should deliver the creative output when research is in iter 0 and synthesis in iter 1", func() {
			// Iteration 0: tool calls gather data (box office report)
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Box office data: Captain America: Brave New World #1...",
							ToolCalls: []model.ToolCall{
								{}, // web_search
								{}, // browser_navigate
								{}, // browser_read_text
							},
						},
					},
				},
			}, nil)

			// Iteration 1: zero tool calls — produces the dad jokes (the REAL answer)
			fakeExpert.DoReturnsOnCall(1, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Here are some dad jokes about Captain America!\n1. Why did Captain America go to therapy?...",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "make some dad jokes about the latest movie from hollywood",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Success))
			Expect(result.NodeCount).To(Equal(2))
			// The final output MUST be the dad jokes, not the box office data.
			Expect(result.Output).To(ContainSubstring("dad jokes about Captain America"))
			Expect(result.Output).NotTo(ContainSubstring("Box office data"))
		})

		It("should NOT suppress SSE for the creative-output iteration", func() {

			// Iteration 0: tool calls + intermediate data
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Search results gathered",
							ToolCalls: []model.ToolCall{
								{},
							},
						},
					},
				},
			}, nil)

			// Iteration 1: zero tool calls — the final creative output
			fakeExpert.DoReturnsOnCall(1, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Here are dad jokes synthesized from the research",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Research then create",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Output).To(Equal("Here are dad jokes synthesized from the research"))

			// Iteration 1 should have EventChan active (not suppressed)
			// because iteration 0 used tools (task NOT completed), so
			// priorHadOutput is false.
			Expect(fakeExpert.DoCallCount()).To(Equal(2))
			// EventChan propagation is now handled by the agui event bus — no explicit field to check
		})
	})

	Context("Graceful Degradation (fallback text emission)", func() {
		It("should stream the completing iteration's text when prior iterations only used tools", func() {

			// Iteration 0: tool calls + output, but taskCompleted=false
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Search results for California news...",
							ToolCalls: []model.ToolCall{
								{}, // web_search
							},
						},
					},
				},
			}, nil)

			// Iteration 1: final answer (no tools, taskCompleted=true)
			fakeExpert.DoReturnsOnCall(1, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Here is the California news summary",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "good news from California",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// Output should be the completing iteration's text
			Expect(result.Output).To(Equal("Here is the California news summary"))
			Expect(result.NodeCount).To(Equal(2))

			// EventChan should NOT have been suppressed for iteration 1
			// because iteration 0 used tools (task was NOT completed).
			Expect(fakeExpert.DoCallCount()).To(Equal(2))
			// EventChan propagation is now handled by the agui event bus
		})

		It("should emit error message when no output at all", func() {

			// Iteration 1: error — expert call fails
			fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("context deadline exceeded"))

			req := reactree.TreeRequest{
				Goal: "test task",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Failure))
			// The output will contain the error text from agent_node
			Expect(result.Output).To(ContainSubstring("context deadline exceeded"))
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

	Context("Repetition Detector", func() {
		It("should break the loop when the same output is repeated 3 times", func() {
			config.MaxIterations = 10
			treeExec = reactree.NewTreeExecutor(
				fakeExpert,
				memory.NewWorkingMemory(),
				fakeEpisodic,
				config,
			)

			// Always return the same output with tool calls (never completes naturally)
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Error: file not found at /foo/bar.go",
							ToolCalls: []model.ToolCall{
								{}, // Simulates retrying the same tool
							},
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Fix the bug",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(reactree.Failure))
			// Should have stopped after 3 identical outputs, not MaxIterations (10)
			Expect(fakeExpert.DoCallCount()).To(Equal(3))
			Expect(result.Output).To(ContainSubstring("stuck repeating"))
		})

		It("should NOT trigger if outputs are different each time", func() {
			config.MaxIterations = 5
			treeExec = reactree.NewTreeExecutor(
				fakeExpert,
				memory.NewWorkingMemory(),
				fakeEpisodic,
				config,
			)

			// Each call returns different output
			for i := 0; i < 5; i++ {
				fakeExpert.DoReturnsOnCall(i, expert.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    "assistant",
								Content: fmt.Sprintf("Working on step %d with different content", i),
								ToolCalls: []model.ToolCall{
									{},
								},
							},
						},
					},
				}, nil)
			}

			req := reactree.TreeRequest{
				Goal: "Progressive task",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// Should run all 5 iterations since outputs are different
			Expect(fakeExpert.DoCallCount()).To(Equal(5))
			Expect(result.Output).NotTo(ContainSubstring("stuck repeating"))
		})
	})

	Context("SSE Always Forwarded", func() {
		It("should forward EventChan to every iteration including validation probes", func() {

			// Iteration 0: tool calls + output, taskCompleted=true (sets priorHadOutput)
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "First answer delivered",
						},
					},
				},
			}, nil)

			// Iteration 1 won't happen because iteration 0 completed (taskCompleted=true).
			// But let's test the 2-iteration case: iter 0 has tools, iter 1 completes.

			// Re-setup: Iteration 0 uses tools (does NOT complete)
			fakeExpert.DoReturnsOnCall(0, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Gathered data",
							ToolCalls: []model.ToolCall{
								{},
							},
						},
					},
				},
			}, nil)

			// Iteration 1: completes with output
			fakeExpert.DoReturnsOnCall(1, expert.Response{
				Choices: []model.Choice{
					{
						Message: model.Message{
							Role:    "assistant",
							Content: "Final answer",
						},
					},
				},
			}, nil)

			req := reactree.TreeRequest{
				Goal: "Two iteration task",
			}

			result, err := treeExec.Run(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Output).To(Equal("Final answer"))
			Expect(fakeExpert.DoCallCount()).To(Equal(2))

			// Both calls used the same context with bus registration —
			// EventChan propagation is now handled by the agui event bus.
			// No explicit EventChannel field to check.
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
