// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("Loop State", func() {
	Describe("MarkCompressed / GetChunkBoost", func() {
		It("records compression info", func() {
			ls := &loopState{chunkBoosts: map[string]int{}}
			ls.MarkCompressed("web_search", 5000, 1000)
			Expect(ls.lastCompressedTool).To(Equal("web_search"))
			Expect(ls.lastOriginalSize).To(Equal(5000))
		})

		DescribeTable("GetChunkBoost",
			func(boosts map[string]int, key string, expected int) {
				ls := &loopState{chunkBoosts: boosts}
				Expect(ls.GetChunkBoost(key)).To(Equal(expected))
			},
			Entry("no boost", map[string]int{}, "web_search", 0),
			Entry("has boost", map[string]int{"web_search": 2}, "web_search", 2),
		)
	})

	Describe("toolsForIteration", func() {
		stubTool := func(name string) tool.Tool {
			f := &toolsfakes.FakeCallableTool{}
			f.DeclarationReturns(&tool.Declaration{Name: name})
			return f
		}

		It("returns all tools when no budgets", func() {
			ls := &loopState{toolBudgets: map[string]int{}}
			Expect(ls.toolsForIteration([]tool.Tool{stubTool("a"), stubTool("b")})).To(HaveLen(2))
		})

		It("removes exceeded-budget tools", func() {
			ls := &loopState{
				toolBudgets:    map[string]int{"a": 3},
				toolCallCounts: map[string]int{"a": 3},
			}
			result := ls.toolsForIteration([]tool.Tool{stubTool("a"), stubTool("b")})
			Expect(result).To(HaveLen(1))
			Expect(result[0].Declaration().Name).To(Equal("b"))
		})
	})

	Describe("budgetExhaustedTools", func() {
		It("returns exceeded names", func() {
			ls := &loopState{
				toolBudgets:    map[string]int{"a": 3, "b": 2},
				toolCallCounts: map[string]int{"a": 3, "b": 5},
			}
			Expect(ls.budgetExhaustedTools()).To(ConsistOf("a", "b"))
		})
	})

	Describe("checkRepetition", func() {
		It("returns false on first non-empty output", func() {
			// Arrange
			ls := &loopState{capturedOutput: "hello"}

			// Act
			result := ls.checkRepetition()

			// Assert
			Expect(result).To(BeFalse())
		})

		It("returns false on empty output", func() {
			// Arrange
			ls := &loopState{capturedOutput: ""}

			// Act
			result := ls.checkRepetition()

			// Assert
			Expect(result).To(BeFalse())
		})

		It("detects repeated outputs", func() {
			ls := &loopState{}
			for i := 0; i < maxRepetitions; i++ {
				ls.capturedOutput = "same"
				ls.checkRepetition()
			}
			Expect(ls.repetitionCount).To(BeNumerically(">=", maxRepetitions))
		})

		It("resets count on different output", func() {
			ls := &loopState{capturedOutput: "a"}
			ls.checkRepetition()
			ls.capturedOutput = "b"
			ls.checkRepetition()
			Expect(ls.repetitionCount).To(Equal(1))
		})
	})

	Describe("toResult", func() {
		It("maps fields", func() {
			ls := &loopState{lastStatus: Success, lastOutput: "out", iteration: 5}
			r := ls.toResult()
			Expect(r.Status).To(Equal(Success))
			Expect(r.Output).To(Equal("out"))
			Expect(r.NodeCount).To(Equal(5))
		})
	})

	Describe("computeConfidence", func() {
		DescribeTable("returns expected score",
			func(ls loopState, expected float64) {
				Expect(ls.computeConfidence()).To(BeNumerically("~", expected, 0.001))
			},
			Entry("perfect run: task completed + success + 1/3 iterations + no repetition + output",
				loopState{
					capturedTaskCompleted: true,
					lastStatus:            Success,
					lastOutput:            "done",
					iteration:             1,
					maxIterations:         3,
				},
				// 0.4 + 0.2 + (1 - 1/3)*0.2 + 0.1 + 0.1 = 0.933
				0.933,
			),
			Entry("success without task completion (error-like output)",
				loopState{
					capturedTaskCompleted: false,
					lastStatus:            Success,
					lastOutput:            "error: something failed",
					iteration:             1,
					maxIterations:         3,
				},
				// 0.0 + 0.2 + (1 - 1/3)*0.2 + 0.1 + 0.1 = 0.533
				0.533,
			),
			Entry("failure status — below threshold",
				loopState{
					capturedTaskCompleted: false,
					lastStatus:            Failure,
					lastOutput:            "I got stuck",
					iteration:             3,
					maxIterations:         3,
				},
				// 0.0 + 0.0 + (1 - 3/3)*0.2 + 0.1 + 0.1 = 0.2
				0.2,
			),
			Entry("success at max iterations — low efficiency",
				loopState{
					capturedTaskCompleted: true,
					lastStatus:            Success,
					lastOutput:            "finally done",
					iteration:             3,
					maxIterations:         3,
				},
				// 0.4 + 0.2 + 0.0 + 0.1 + 0.1 = 0.8
				0.8,
			),
			Entry("stuck in repetition loop",
				loopState{
					capturedTaskCompleted: false,
					lastStatus:            Failure,
					lastOutput:            "repeating",
					iteration:             3,
					maxIterations:         3,
					repetitionCount:       maxRepetitions,
				},
				// 0.0 + 0.0 + 0.0 + 0.0 + 0.1 = 0.1
				0.1,
			),
			Entry("empty output",
				loopState{
					capturedTaskCompleted: false,
					lastStatus:            Success,
					lastOutput:            "",
					iteration:             1,
					maxIterations:         3,
				},
				// 0.0 + 0.2 + (1 - 1/3)*0.2 + 0.1 + 0.0 = 0.433
				0.433,
			),
			Entry("zero maxIterations (edge case)",
				loopState{
					capturedTaskCompleted: true,
					lastStatus:            Success,
					lastOutput:            "done",
					iteration:             1,
					maxIterations:         0,
				},
				// 0.4 + 0.2 + 0.0 (skipped) + 0.1 + 0.1 = 0.8
				0.8,
			),
		)
	})

	Describe("accumulateContext", func() {
		It("appends output", func() {
			ls := &loopState{iteration: 1, capturedOutput: "found 3 files"}
			ls.accumulateContext()
			Expect(ls.contextBuffer.String()).To(ContainSubstring("Iteration 1"))
		})

		It("truncates large output", func() {
			ls := &loopState{iteration: 2, capturedOutput: strings.Repeat("x", 4000)}
			ls.accumulateContext()
			Expect(ls.contextBuffer.String()).To(ContainSubstring("truncated"))
		})

		It("skips empty output", func() {
			ls := &loopState{iteration: 1}
			ls.accumulateContext()
			Expect(ls.contextBuffer.String()).To(BeEmpty())
		})
	})
})

var _ = Describe("Tree internals", func() {
	Describe("ensureUserFeedback", func() {
		It("exits early when streamed (no panic)", func() {
			// Arrange
			ls := &loopState{textWasStreamed: true}

			// Act + Assert (no panic, no side effects)
			Expect(func() {
				(&tree{}).ensureUserFeedback(context.Background(), ls)
			}).NotTo(Panic())
		})

		It("exits early with no agui channel (no panic)", func() {
			// Arrange
			ls := &loopState{lastOutput: "something"}

			// Act + Assert (no panic, no side effects)
			Expect(func() {
				(&tree{}).ensureUserFeedback(context.Background(), ls)
			}).NotTo(Panic())
		})

		DescribeTable("emits correct message with channel",
			func(lastOutput string, bufContent string) {
				ch := make(chan<- interface{}, 10)
				origin := messenger.MessageOrigin{
					Platform: "test",
					Channel:  messenger.Channel{ID: lastOutput + bufContent},
					Sender:   messenger.Sender{ID: "s"},
				}
				agui.Register(origin, ch)
				defer agui.Deregister(origin)
				ctx := messenger.WithMessageOrigin(context.Background(), origin)

				var buf strings.Builder
				buf.WriteString(bufContent)
				(&tree{}).ensureUserFeedback(ctx, &loopState{
					lastOutput: lastOutput, contextBuffer: buf,
				})
			},
			Entry("with lastOutput", "final", ""),
			Entry("with contextBuffer", "", "partial"),
			Entry("default error", "", ""),
		)
	})

	Describe("emitIterationProgress", func() {
		It("no-ops without channel (no panic)", func() {
			// Arrange
			ls := &loopState{iteration: 1, maxIterations: 5}

			// Act + Assert
			Expect(func() {
				(&tree{}).emitIterationProgress(context.Background(), ls)
			}).NotTo(Panic())
		})

		It("emits with channel (no panic)", func() {
			// Arrange
			ch := make(chan<- interface{}, 10)
			origin := messenger.MessageOrigin{
				Platform: "test", Channel: messenger.Channel{ID: "prog"},
				Sender: messenger.Sender{ID: "s"},
			}
			agui.Register(origin, ch)
			defer agui.Deregister(origin)
			ctx := messenger.WithMessageOrigin(context.Background(), origin)

			// Act + Assert
			Expect(func() {
				(&tree{}).emitIterationProgress(ctx, &loopState{iteration: 2, maxIterations: 5})
			}).NotTo(Panic())
		})
	})

	Describe("memory resolution", func() {
		It("resolveWorkingMemory prefers request-level", func() {
			reqWM := memory.NewWorkingMemory()
			t := &tree{workingMemory: memory.NewWorkingMemory()}
			Expect(t.resolveWorkingMemory(TreeRequest{WorkingMemory: reqWM})).To(Equal(reqWM))
		})

		It("resolveWorkingMemory falls back to tree-level", func() {
			treeWM := memory.NewWorkingMemory()
			Expect((&tree{workingMemory: treeWM}).resolveWorkingMemory(TreeRequest{})).To(Equal(treeWM))
		})

		It("resolveEpisodic prefers request-level", func() {
			reqEp := &memoryfakes.FakeEpisodicMemory{}
			t := &tree{episodic: &memoryfakes.FakeEpisodicMemory{}}
			Expect(t.resolveEpisodic(TreeRequest{EpisodicMemory: reqEp})).To(Equal(reqEp))
		})

		It("resolveEpisodic falls back to tree-level", func() {
			ep := &memoryfakes.FakeEpisodicMemory{}
			Expect((&tree{episodic: ep}).resolveEpisodic(TreeRequest{})).To(Equal(ep))
		})
	})

	Describe("NewTreeExecutor", func() {
		It("handles nil dependencies", func() {
			Expect(NewTreeExecutor(&expertfakes.FakeExpert{}, nil, nil, TreeConfig{})).NotTo(BeNil())
		})
	})

	Describe("runSingleNode integration", func() {
		It("runs and returns result", func() {
			fakeExpert := &expertfakes.FakeExpert{}
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: "assistant", Content: "Single node"}},
				},
			}, nil)
			executor := NewTreeExecutor(fakeExpert, memory.NewWorkingMemory(),
				&memoryfakes.FakeEpisodicMemory{},
				TreeConfig{MaxIterations: 0, MaxDecisionsPerNode: 5, MaxTotalNodes: 10})

			result, err := executor.Run(context.Background(), TreeRequest{Goal: "task"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(Success))
			Expect(result.NodeCount).To(Equal(1))
		})

		It("handles expert error", func() {
			fakeExpert := &expertfakes.FakeExpert{}
			fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("timeout"))
			result, err := NewTreeExecutor(fakeExpert, nil, nil,
				TreeConfig{MaxIterations: 0, MaxDecisionsPerNode: 5, MaxTotalNodes: 10}).
				Run(context.Background(), TreeRequest{Goal: "fail"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(Failure))
		})

		It("runs with dry-run simulation", func() {
			fakeExpert := &expertfakes.FakeExpert{}
			fakeExpert.DoReturns(expert.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: "assistant", Content: "Dry-run result"}},
				},
			}, nil)
			cfg := TreeConfig{MaxIterations: 0, MaxDecisionsPerNode: 5, MaxTotalNodes: 10}
			cfg.Toggles.Features.DryRun.Enabled = true
			result, err := NewTreeExecutor(fakeExpert, nil, nil, cfg).
				Run(context.Background(), TreeRequest{Goal: "dry-run"})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(Success))
		})
	})
})
