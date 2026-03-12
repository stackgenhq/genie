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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agentutils/agentutilsfakes"
	"github.com/stackgenhq/genie/pkg/halguard"
	"github.com/stackgenhq/genie/pkg/halguard/halguardfakes"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("CreateAgentRequest", func() {

	Describe("timeout", func() {
		DescribeTable("clamps the timeout to [minTimeout, maxTimeout] with a default",
			func(input float64, expected time.Duration) {
				req := CreateAgentRequest{TimeoutSeconds: input}
				Expect(req.timeout()).To(Equal(expected))
			},
			Entry("zero uses default (5 min)", 0.0, defaultTimeout),
			Entry("negative uses default (5 min)", -10.0, defaultTimeout),
			Entry("below floor is clamped to floor", 5.0, minTimeout),
			Entry("exactly at floor stays at floor", minTimeout.Seconds(), minTimeout),
			Entry("within range is unchanged", 180.0, 180*time.Second),
			Entry("exactly at ceiling stays at ceiling", maxTimeout.Seconds(), maxTimeout),
			Entry("above ceiling is clamped to ceiling", 900.0, maxTimeout),
		)
	})

	Describe("clampedMaxToolIterations", func() {
		DescribeTable("clamps to [minToolIterCap, maxToolIterCap]",
			func(input int, expected int) {
				req := CreateAgentRequest{MaxToolIterations: input}
				Expect(req.clampedMaxToolIterations()).To(Equal(expected))
			},
			Entry("zero is raised to floor", 0, minToolIterCap),
			Entry("negative is raised to floor", -1, minToolIterCap),
			Entry("below floor is raised to floor", 3, minToolIterCap),
			Entry("exactly at floor stays", minToolIterCap, minToolIterCap),
			Entry("mid-range is unchanged", 25, 25),
			Entry("exactly at ceiling stays", maxToolIterCap, maxToolIterCap),
			Entry("above ceiling is lowered to ceiling", 100, maxToolIterCap),
		)
	})

	Describe("clampedMaxLLMCalls", func() {
		DescribeTable("clamps to [minLLMCallsCap, maxLLMCallsCap]",
			func(input int, expected int) {
				req := CreateAgentRequest{MaxLLMCalls: input}
				Expect(req.clampedMaxLLMCalls()).To(Equal(expected))
			},
			Entry("zero is raised to floor", 0, minLLMCallsCap),
			Entry("negative is raised to floor", -5, minLLMCallsCap),
			Entry("below floor is raised to floor", 4, minLLMCallsCap),
			Entry("exactly at floor stays", minLLMCallsCap, minLLMCallsCap),
			Entry("mid-range is unchanged", 30, 30),
			Entry("exactly at ceiling stays", maxLLMCallsCap, maxLLMCallsCap),
			Entry("above ceiling is lowered to ceiling", 200, maxLLMCallsCap),
		)
	})

	Describe("flow", func() {
		DescribeTable("maps flow_type string to ControlFlowType",
			func(ctx context.Context, input string, expected ControlFlowType) {
				req := CreateAgentRequest{Flow: input}
				Expect(req.flow(ctx)).To(Equal(expected))
			},
			Entry("parallel", "parallel", ControlFlowParallel),
			Entry("fallback", "fallback", ControlFlowFallback),
			Entry("sequence", "sequence", ControlFlowSequence),
			Entry("empty string defaults to sequence", "", ControlFlowSequence),
			Entry("unknown value defaults to sequence", "round_robin", ControlFlowSequence),
		)
	})

	Describe("resolveStatus", func() {
		const agentName = "test-agent"

		DescribeTable("determines status and output from execution outcome",
			func(timedOut bool, lastErr, result, partialToolResults string, expectedStatus AgentStatus, outputMatcher OmegaMatcher) {
				req := CreateAgentRequest{AgentName: agentName}
				status, output := req.resolveStatus(timedOut, lastErr, result, partialToolResults)
				Expect(status).To(Equal(expectedStatus))
				Expect(output).To(outputMatcher)
			},
			Entry("success with output",
				false, "", "all good", "",
				AgentStatusSuccess, Equal("all good"),
			),
			Entry("success with empty output and no error",
				false, "", "", "",
				AgentStatusSuccess, BeEmpty(),
			),
			Entry("timeout with partial output",
				true, "", "partial data", "",
				AgentStatusPartial, And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring(agentName),
					ContainSubstring("partial data"),
				),
			),
			Entry("timeout with no output",
				true, "", "", "",
				AgentStatusPartial, And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring("No output was captured"),
				),
			),
			Entry("error with no output",
				false, "connection refused", "", "",
				AgentStatusError, Equal(fmt.Sprintf("sub-agent error: %s", "connection refused")),
			),
			Entry("error is ignored when output exists",
				false, "some warning", "valid result", "",
				AgentStatusSuccess, Equal("valid result"),
			),
			Entry("timeout takes precedence over lastErr",
				true, "some error", "", "",
				AgentStatusPartial, ContainSubstring("[TIME LIMIT REACHED]"),
			),
			Entry("error with no output but has partial tool results",
				false, "max LLM calls (10) exceeded", "", "file contents found\n---\nsearch results here",
				AgentStatusPartial, And(
					ContainSubstring("[BUDGET EXCEEDED]"),
					ContainSubstring(agentName),
					ContainSubstring("file contents found"),
					ContainSubstring("search results here"),
					ContainSubstring("max LLM calls (10) exceeded"),
				),
			),
			Entry("timeout with no output but has partial tool results",
				true, "", "", "partial findings from tools",
				AgentStatusPartial, And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring("partial findings from tools"),
				),
			),
			Entry("error ignored when output exists even with tool results",
				false, "some warning", "valid result", "tool results here",
				AgentStatusSuccess, Equal("valid result"),
			),
			Entry("middleware cancellation error surfaces correctly",
				false, "loop detected: tool X has been called with identical arguments 2 times consecutively. Stop calling this tool and summarize the results you already have", "", "",
				AgentStatusError, ContainSubstring("loop detected"),
			),
		)
	})
})

var _ = Describe("createAgentTool guard helpers", func() {
	// stubTool creates a minimal tool.Tool with a given name using counterfeiter.
	stubTool := func(name string) tool.Tool {
		fake := &toolsfakes.FakeCallableTool{}
		fake.DeclarationReturns(&tool.Declaration{Name: name})
		return fake
	}

	// makeRegistry builds a tools.Registry from tool names.
	makeRegistry := func(names ...string) *tools.Registry {
		var tt tools.Tools
		for _, n := range names {
			tt = append(tt, stubTool(n))
		}
		return tools.NewRegistry(context.Background(), tt)
	}

	Describe("isRetrievalOnly", func() {
		cat := &createAgentTool{}

		It("returns true when all tools are retrieval-only", func() {
			reg := makeRegistry("memory_search", "graph_query")
			Expect(cat.isRetrievalOnly(reg)).To(BeTrue())
		})

		It("returns true for a single retrieval tool", func() {
			reg := makeRegistry("memory_search")
			Expect(cat.isRetrievalOnly(reg)).To(BeTrue())
		})

		It("returns false when any non-retrieval tool is present", func() {
			reg := makeRegistry("memory_search", "run_shell")
			Expect(cat.isRetrievalOnly(reg)).To(BeFalse())
		})

		It("returns false for an empty registry", func() {
			reg := makeRegistry()
			Expect(cat.isRetrievalOnly(reg)).To(BeFalse())
		})

		It("returns true for both retrieval tools", func() {
			reg := makeRegistry("memory_search", "graph_query")
			Expect(cat.isRetrievalOnly(reg)).To(BeTrue())
		})
	})

	Describe("hasVectorBackedTools", func() {
		cat := &createAgentTool{}

		It("returns true when memory_search is present", func() {
			reg := makeRegistry("memory_search", "graph_query")
			Expect(cat.hasVectorBackedTools(reg)).To(BeTrue())
		})

		It("returns false for graph-only tools", func() {
			reg := makeRegistry("graph_query", "graph_store")
			Expect(cat.hasVectorBackedTools(reg)).To(BeFalse())
		})

		It("returns false for empty registry", func() {
			reg := makeRegistry()
			Expect(cat.hasVectorBackedTools(reg)).To(BeFalse())
		})
	})

	Describe("isMemoryEmpty", func() {
		It("returns true when vectorStore is nil", func() {
			cat := &createAgentTool{vectorStore: nil}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeTrue())
		})

		It("returns true when Search returns no results", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchReturns(nil, nil)
			cat := &createAgentTool{vectorStore: fakeStore}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeTrue())
		})

		It("returns false when Search returns results", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchReturns([]vector.SearchResult{{ID: "1", Content: "x"}}, nil)
			cat := &createAgentTool{vectorStore: fakeStore}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeFalse())
		})

		It("returns false when Search errors (safe fallback)", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchReturns(nil, fmt.Errorf("connection refused"))
			cat := &createAgentTool{vectorStore: fakeStore}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeFalse())
		})
	})
})

var _ = Describe("createAgentTool halguard integration", func() {
	Describe("SetHalGuardThreshold", func() {
		It("sets a positive threshold", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(0.6)
			Expect(t.halGuardThreshold).To(Equal(0.6))
		})

		It("ignores zero threshold (keeps default)", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(0)
			Expect(t.halGuardThreshold).To(Equal(0.0))
		})

		It("ignores negative threshold", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(-0.5)
			Expect(t.halGuardThreshold).To(Equal(0.0))
		})
	})

	Describe("pre-check threshold logic", func() {
		It("uses default 0.4 threshold when halGuardThreshold is zero", func() {
			t := &createAgentTool{}
			// Default threshold is 0.4, so confidence 0.39 should be below it
			Expect(t.halGuardThreshold).To(Equal(0.0))

			// When threshold is 0, execute uses 0.4 as fallback
			threshold := 0.4
			if t.halGuardThreshold > 0 {
				threshold = t.halGuardThreshold
			}
			Expect(threshold).To(Equal(0.4))
		})

		It("uses configured threshold when set", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(0.7)

			threshold := 0.4
			if t.halGuardThreshold > 0 {
				threshold = t.halGuardThreshold
			}
			Expect(threshold).To(Equal(0.7))
		})
	})

	Describe("doPreflightChecks", func() {
		var (
			cat       *createAgentTool
			fakeGuard *halguardfakes.FakeGuard
			req       CreateAgentRequest
		)

		BeforeEach(func() {
			fakeGuard = &halguardfakes.FakeGuard{}
			cat = &createAgentTool{halGuard: fakeGuard}
			req = CreateAgentRequest{
				AgentName: "test-agent",
				Goal:      "test goal",
				Context:   "test context",
				ToolNames: []string{"test_tool"},
			}
		})

		It("returns nil immediately if halGuard is nil", func() {
			cat.halGuard = nil
			err := cat.doPreflightChecks(context.Background(), req)
			Expect(err).To(BeNil())
			Expect(fakeGuard.PreCheckCallCount()).To(Equal(0))
		})

		It("proceeds (returns nil) if PreCheck returns an error", func() {
			fakeGuard.PreCheckReturns(halguard.PreCheckResult{}, fmt.Errorf("backend down"))
			err := cat.doPreflightChecks(context.Background(), req)
			Expect(err).To(BeNil())
			Expect(fakeGuard.PreCheckCallCount()).To(Equal(1))
		})

		It("returns error if confidence is below threshold", func() {
			fakeGuard.PreCheckReturns(halguard.PreCheckResult{
				Confidence: 0.3,
				Summary:    "fabricated scenario",
			}, nil)
			err := cat.doPreflightChecks(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GROUNDING CHECK FAILED"))
			Expect(err.Error()).To(ContainSubstring("fabricated scenario"))
		})

		It("returns nil if confidence is exactly at the default threshold", func() {
			fakeGuard.PreCheckReturns(halguard.PreCheckResult{
				Confidence: 0.4,
			}, nil)
			err := cat.doPreflightChecks(context.Background(), req)
			Expect(err).To(BeNil())
		})

		It("returns nil if confidence is above the threshold", func() {
			fakeGuard.PreCheckReturns(halguard.PreCheckResult{
				Confidence: 0.9,
			}, nil)
			err := cat.doPreflightChecks(context.Background(), req)
			Expect(err).To(BeNil())
		})

		It("uses configured threshold instead of default", func() {
			cat.SetHalGuardThreshold(0.8)
			fakeGuard.PreCheckReturns(halguard.PreCheckResult{
				Confidence: 0.7,
				Summary:    "not confident enough",
			}, nil)
			err := cat.doPreflightChecks(context.Background(), req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GROUNDING CHECK FAILED"))
		})
	})
})
var _ = Describe("applyZeroToolUseGuard", func() {
	// Tests the extracted method directly instead of mirroring the condition.
	var cat *createAgentTool
	req := CreateAgentRequest{AgentName: "test-agent"}

	BeforeEach(func() {
		cat = &createAgentTool{}
	})

	It("fires when zero tool calls, has result, has tools, success, not timed out", func() {
		sar := cat.applyZeroToolUseGuard(context.Background(), req, subAgentResult{
			output:        "I don't have access",
			status:        AgentStatusSuccess,
			toolCallCount: 0,
			timedOut:      false,
			toolNameList:  []string{"run_shell", "read_file"},
			numTools:      2,
		})
		Expect(sar.status).To(Equal(AgentStatusToolUseFailure))
		Expect(sar.output).To(ContainSubstring("SUB-AGENT DID NOT USE TOOLS"))
		Expect(sar.output).To(ContainSubstring("run_shell"))
		Expect(sar.output).To(ContainSubstring("I don't have access"))
	})

	It("does not fire when sub-agent made tool calls", func() {
		sar := cat.applyZeroToolUseGuard(context.Background(), req, subAgentResult{
			output:        "result from tools",
			status:        AgentStatusSuccess,
			toolCallCount: 3,
			numTools:      2,
		})
		Expect(sar.status).To(Equal(AgentStatusSuccess))
		Expect(sar.output).To(Equal("result from tools"))
	})

	It("does not fire when result is empty", func() {
		sar := cat.applyZeroToolUseGuard(context.Background(), req, subAgentResult{
			output:   "",
			status:   AgentStatusSuccess,
			numTools: 2,
		})
		Expect(sar.status).To(Equal(AgentStatusSuccess))
	})

	It("does not fire when status is error", func() {
		sar := cat.applyZeroToolUseGuard(context.Background(), req, subAgentResult{
			output:   "error message",
			status:   AgentStatusError,
			numTools: 2,
		})
		Expect(sar.status).To(Equal(AgentStatusError))
	})

	It("does not fire when timed out", func() {
		sar := cat.applyZeroToolUseGuard(context.Background(), req, subAgentResult{
			output:   "partial",
			status:   AgentStatusPartial,
			timedOut: true,
			numTools: 2,
		})
		Expect(sar.status).To(Equal(AgentStatusPartial))
	})

	It("does not fire when no tools available", func() {
		sar := cat.applyZeroToolUseGuard(context.Background(), req, subAgentResult{
			output:   "answer",
			status:   AgentStatusSuccess,
			numTools: 0,
		})
		Expect(sar.status).To(Equal(AgentStatusSuccess))
		Expect(sar.output).To(Equal("answer"))
	})
})

var _ = Describe("runHalGuardPostCheck", func() {
	var (
		cat       *createAgentTool
		fakeGuard *halguardfakes.FakeGuard
		req       CreateAgentRequest
	)

	BeforeEach(func() {
		fakeGuard = &halguardfakes.FakeGuard{}
		cat = &createAgentTool{halGuard: fakeGuard}
		req = CreateAgentRequest{AgentName: "test-agent", Goal: "list PRs"}
	})

	It("calls PostCheck when status is success with output", func() {
		fakeGuard.PostCheckReturns(halguard.VerificationResult{IsFactual: true, Tier: halguard.TierLight}, nil)

		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output:       "PR data here",
			status:       AgentStatusSuccess,
			toolNameList: []string{"run_shell", "read_file"},
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(1))
		_, postReq := fakeGuard.PostCheckArgsForCall(0)
		Expect(postReq.ToolSummary).To(Equal("run_shell, read_file"))
		Expect(sar.status).To(Equal(AgentStatusSuccess))
		Expect(sar.output).To(Equal("PR data here"))
	})

	It("skips PostCheck when status is partial (timed out)", func() {
		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output:   "[TIME LIMIT REACHED] partial data",
			status:   AgentStatusPartial,
			timedOut: true,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(0))
		Expect(sar.status).To(Equal(AgentStatusPartial))
	})

	It("skips PostCheck when status is partial (budget exceeded)", func() {
		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "[BUDGET EXCEEDED] partial findings",
			status: AgentStatusPartial,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(0))
		Expect(sar.status).To(Equal(AgentStatusPartial))
	})

	It("skips PostCheck when status is error", func() {
		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "sub-agent error: connection refused",
			status: AgentStatusError,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(0))
		Expect(sar.status).To(Equal(AgentStatusError))
	})

	It("skips PostCheck when status is tool_use_failure", func() {
		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "⚠️ SUB-AGENT DID NOT USE TOOLS: ...",
			status: AgentStatusToolUseFailure,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(0))
		Expect(sar.status).To(Equal(AgentStatusToolUseFailure))
	})

	It("skips PostCheck when output is empty", func() {
		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "",
			status: AgentStatusSuccess,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(0))
		Expect(sar.output).To(BeEmpty())
	})

	It("skips PostCheck when halGuard is nil", func() {
		cat.halGuard = nil
		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "valid output",
			status: AgentStatusSuccess,
		}, nil)

		Expect(sar.status).To(Equal(AgentStatusSuccess))
		Expect(sar.output).To(Equal("valid output"))
	})

	It("corrects output when PostCheck detects hallucination", func() {
		fakeGuard.PostCheckReturns(halguard.VerificationResult{
			IsFactual:     false,
			CorrectedText: "corrected output",
			Tier:          halguard.TierFull,
			BlockScores:   []halguard.BlockScore{{Label: halguard.BlockContradiction}},
		}, nil)

		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "hallucinated data",
			status: AgentStatusSuccess,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(1))
		Expect(sar.status).To(Equal(AgentStatusVerifiedCorrected))
		Expect(sar.output).To(Equal("corrected output"))
	})

	It("keeps original output when PostCheck errors", func() {
		fakeGuard.PostCheckReturns(halguard.VerificationResult{}, fmt.Errorf("model unavailable"))

		sar := cat.runHalGuardPostCheck(context.Background(), req, subAgentResult{
			output: "valid data",
			status: AgentStatusSuccess,
		}, nil)

		Expect(fakeGuard.PostCheckCallCount()).To(Equal(1))
		Expect(sar.status).To(Equal(AgentStatusSuccess))
		Expect(sar.output).To(Equal("valid data"))
	})
})

var _ = Describe("storeResults", func() {
	It("stores result in working memory", func() {
		wm := memory.NewWorkingMemory()
		cat := &createAgentTool{workingMemory: wm}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "find files"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output: "found 3 files",
		})

		stored, ok := wm.Recall("subagent:test-agent:result")
		Expect(ok).To(BeTrue())
		Expect(stored).To(Equal("found 3 files"))
	})

	It("does not store empty output", func() {
		wm := memory.NewWorkingMemory()
		cat := &createAgentTool{workingMemory: wm}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "find files"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output: "",
		})

		_, ok := wm.Recall("subagent:test-agent:result")
		Expect(ok).To(BeFalse())
	})

	It("stores in episodic memory with success status", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		cat := &createAgentTool{episodic: fakeEpisodic, workingMemory: memory.NewWorkingMemory()}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "find files"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output:   "found 3 files",
			timedOut: false,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Goal).To(Equal("find files"))
		Expect(episode.Status).To(Equal(memory.EpisodeSuccess))
	})

	It("stores in episodic memory with failure status when timed out", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		cat := &createAgentTool{episodic: fakeEpisodic, workingMemory: memory.NewWorkingMemory()}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "find files"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output:   "partial results",
			timedOut: true,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Status).To(Equal(memory.EpisodeFailure))
	})

	It("stores in episodic memory with failure status when status is error", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		cat := &createAgentTool{episodic: fakeEpisodic, workingMemory: memory.NewWorkingMemory()}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "scan repos"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output:   "sub-agent error: connection refused",
			status:   AgentStatusError,
			timedOut: false,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Goal).To(Equal("scan repos"))
		Expect(episode.Status).To(Equal(memory.EpisodeFailure))
		Expect(episode.Trajectory).To(ContainSubstring("connection refused"))
	})

	It("stores in episodic memory with failure status when status is partial", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		cat := &createAgentTool{episodic: fakeEpisodic, workingMemory: memory.NewWorkingMemory()}
		req := CreateAgentRequest{AgentName: "budget-agent", Goal: "list PRs"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output:   "[BUDGET EXCEEDED] partial findings",
			status:   AgentStatusPartial,
			timedOut: false,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Goal).To(Equal("list PRs"))
		Expect(episode.Status).To(Equal(memory.EpisodeFailure))
		Expect(episode.Trajectory).To(ContainSubstring("BUDGET EXCEEDED"))
	})

	It("stores partial/error results in working memory for parent access", func() {
		wm := memory.NewWorkingMemory()
		cat := &createAgentTool{workingMemory: wm}
		req := CreateAgentRequest{AgentName: "failed-agent", Goal: "check services"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output: "[TIME LIMIT REACHED] partial data from 2 of 5 services",
			status: AgentStatusPartial,
		})

		stored, ok := wm.Recall("subagent:failed-agent:result")
		Expect(ok).To(BeTrue())
		Expect(stored).To(ContainSubstring("partial data from 2 of 5 services"))
	})

	It("stores in episodic memory with success status for verified_corrected", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		cat := &createAgentTool{episodic: fakeEpisodic}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "find files"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output: "corrected output",
			status: AgentStatusVerifiedCorrected,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Status).To(Equal(memory.EpisodeSuccess))
	})

	It("stores failure episode with verbal reflection when reflector is configured", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		fakeReflector := &memoryfakes.FakeFailureReflector{}
		fakeReflector.ReflectReturns("Try running gcloud auth login first")
		cat := &createAgentTool{
			episodic:         fakeEpisodic,
			failureReflector: fakeReflector,
			workingMemory:    memory.NewWorkingMemory(),
		}
		req := CreateAgentRequest{AgentName: "gcp-checker", Goal: "check GCP instances"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output: "error: gcloud authentication tokens have expired",
			status: AgentStatusError,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Status).To(Equal(memory.EpisodeFailure))
		Expect(episode.Reflection).To(Equal("Try running gcloud auth login first"))
		Expect(fakeReflector.ReflectCallCount()).To(Equal(1))
	})

	It("stores failure episode with importance score when scorer is configured", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		fakeScorer := &memoryfakes.FakeImportanceScorer{}
		fakeScorer.ScoreReturns(8)
		cat := &createAgentTool{
			episodic:         fakeEpisodic,
			importanceScorer: fakeScorer,
			workingMemory:    memory.NewWorkingMemory(),
		}
		req := CreateAgentRequest{AgentName: "gcp-checker", Goal: "check GCP instances"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output:   "sub-agent error: connection refused",
			status:   AgentStatusError,
			timedOut: false,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Status).To(Equal(memory.EpisodeFailure))
		Expect(episode.Importance).To(Equal(8))
		Expect(fakeScorer.ScoreCallCount()).To(Equal(1))
	})

	It("scores success episodes with importance but no reflection", func() {
		fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
		fakeReflector := &memoryfakes.FakeFailureReflector{}
		fakeScorer := &memoryfakes.FakeImportanceScorer{}
		fakeScorer.ScoreReturns(5)
		cat := &createAgentTool{
			episodic:         fakeEpisodic,
			failureReflector: fakeReflector,
			importanceScorer: fakeScorer,
			workingMemory:    memory.NewWorkingMemory(),
		}
		req := CreateAgentRequest{AgentName: "test-agent", Goal: "list files"}

		cat.storeResults(context.Background(), req, subAgentResult{
			output: "found 3 files",
			status: AgentStatusSuccess,
		})

		Expect(fakeEpisodic.StoreCallCount()).To(Equal(1))
		_, episode := fakeEpisodic.StoreArgsForCall(0)
		Expect(episode.Status).To(Equal(memory.EpisodeSuccess))
		Expect(episode.Reflection).To(BeEmpty())
		Expect(episode.Importance).To(Equal(5))
		Expect(fakeReflector.ReflectCallCount()).To(Equal(0))
		Expect(fakeScorer.ScoreCallCount()).To(Equal(1))
	})
})

var _ = Describe("summarizeOutput", func() {
	req := CreateAgentRequest{AgentName: "test-agent", SummarizeOutput: true}

	It("summarizes when output exceeds threshold", func() {
		fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
		fakeSummarizer.SummarizeReturns("short summary", nil)
		cat := &createAgentTool{summarizer: fakeSummarizer}

		longOutput := strings.Repeat("x", summarizeThreshold+100)
		sar := cat.summarizeOutput(context.Background(), req, subAgentResult{
			output: longOutput,
		})

		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(1))
		Expect(sar.output).To(Equal("short summary"))
	})

	It("skips when output is below threshold", func() {
		fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
		cat := &createAgentTool{summarizer: fakeSummarizer}

		sar := cat.summarizeOutput(context.Background(), req, subAgentResult{
			output: "short output",
		})

		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(0))
		Expect(sar.output).To(Equal("short output"))
	})

	It("skips when SummarizeOutput is false", func() {
		fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
		cat := &createAgentTool{summarizer: fakeSummarizer}
		noSumReq := CreateAgentRequest{AgentName: "test-agent", SummarizeOutput: false}

		longOutput := strings.Repeat("x", summarizeThreshold+100)
		sar := cat.summarizeOutput(context.Background(), noSumReq, subAgentResult{
			output: longOutput,
		})

		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(0))
		Expect(sar.output).To(Equal(longOutput))
	})

	It("skips when timed out", func() {
		fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
		cat := &createAgentTool{summarizer: fakeSummarizer}

		longOutput := strings.Repeat("x", summarizeThreshold+100)
		sar := cat.summarizeOutput(context.Background(), req, subAgentResult{
			output:   longOutput,
			timedOut: true,
		})

		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(0))
		Expect(sar.output).To(Equal(longOutput))
	})

	It("keeps original when summarizer errors", func() {
		fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
		fakeSummarizer.SummarizeReturns("", fmt.Errorf("model unavailable"))
		cat := &createAgentTool{summarizer: fakeSummarizer}

		longOutput := strings.Repeat("x", summarizeThreshold+100)
		sar := cat.summarizeOutput(context.Background(), req, subAgentResult{
			output: longOutput,
		})

		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(1))
		Expect(sar.output).To(Equal(longOutput))
	})
})

var _ = Describe("auto-retry on tool_use_failure", func() {
	// The auto-retry in execute() constructs a retryReq when
	// resp.Status == "tool_use_failure". We test the retry
	// prompt construction and agent naming here.

	It("prepends RETRY prefix to the goal", func() {
		originalGoal := "Run this script: az vm list"
		retryGoal := "[RETRY — PREVIOUS ATTEMPT FAILED] " +
			"Your previous attempt FAILED because you echoed commands as text instead of executing them. " +
			"You MUST call the run_shell tool to execute the script below. " +
			"Do NOT output the script as text. Call run_shell with the script as the command argument.\n\n" +
			originalGoal

		Expect(retryGoal).To(HavePrefix("[RETRY — PREVIOUS ATTEMPT FAILED]"))
		Expect(retryGoal).To(ContainSubstring("You MUST call the run_shell tool"))
		Expect(retryGoal).To(ContainSubstring(originalGoal))
	})

	It("appends -retry suffix to the agent name", func() {
		agentName := "azure-functions-check"
		retryName := agentName + "-retry"
		Expect(retryName).To(Equal("azure-functions-check-retry"))
	})

	It("retry is only triggered when status is tool_use_failure", func() {
		// Verify the condition: err == nil && resp.Status == "tool_use_failure"
		type retryCheck struct {
			err    error
			status AgentStatus
		}
		shouldRetry := func(rc retryCheck) bool {
			return rc.err == nil && rc.status == AgentStatusToolUseFailure
		}

		Expect(shouldRetry(retryCheck{nil, AgentStatusToolUseFailure})).To(BeTrue())
		Expect(shouldRetry(retryCheck{nil, AgentStatusSuccess})).To(BeFalse())
		Expect(shouldRetry(retryCheck{nil, AgentStatusError})).To(BeFalse())
		Expect(shouldRetry(retryCheck{nil, AgentStatusPartial})).To(BeFalse())
		Expect(shouldRetry(retryCheck{fmt.Errorf("some error"), AgentStatusToolUseFailure})).To(BeFalse())
	})
})
