// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
)

var _ = Describe("buildAgentPrompt", func() {
	var (
		wm     *memory.WorkingMemory
		fakeEp *memoryfakes.FakeEpisodicMemory
	)

	BeforeEach(func() {
		wm = memory.NewWorkingMemory()
		fakeEp = &memoryfakes.FakeEpisodicMemory{}
	})

	prompt := func(goal, prevOutput, iterCtx string, iterCount int, exhausted []string) string {
		return buildAgentPrompt(context.Background(), goal, wm, fakeEp, nil,
			prevOutput, iterCtx, iterCount, exhausted)
	}

	It("includes Current Task without memory sections when empty", func() {
		p := prompt("find files", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Current Task"))
		Expect(p).NotTo(ContainSubstring("Sub-Agent Results"))
		Expect(p).NotTo(ContainSubstring("Working Memory"))
	})

	It("includes iteration context when present", func() {
		p := prompt("find files", "", "Prior: found 3 files", 2, nil)
		Expect(p).To(ContainSubstring("Progress So Far (iteration 2)"))
		Expect(p).To(ContainSubstring("DO NOT repeat"))
	})

	It("truncates very large iteration context", func() {
		p := prompt("find files", "", strings.Repeat("x", 5000), 3, nil)
		Expect(p).To(ContainSubstring("truncated"))
	})

	It("includes previous stage output when no iteration context", func() {
		p := prompt("find files", "Stage 1 found 3 pods", "", 0, nil)
		Expect(p).To(ContainSubstring("Previous Stage Output"))
	})

	It("prefers iteration context over previous stage output", func() {
		p := prompt("find files", "stage data", "iteration data", 1, nil)
		Expect(p).To(ContainSubstring("Progress So Far"))
		Expect(p).NotTo(ContainSubstring("Previous Stage Output"))
	})

	It("includes budget exhausted tools warning", func() {
		p := prompt("search", "", "", 0, []string{"web_search", "http_request"})
		Expect(p).To(ContainSubstring("Tool Budget Exhausted"))
		Expect(p).To(ContainSubstring("web_search"))
	})

	It("includes sub-agent results from working memory", func() {
		wm.Store("subagent:aws-check:result", "Found 5 EC2 instances")
		p := prompt("summarize", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Sub-Agent Results"))
		Expect(p).To(ContainSubstring("DO NOT SPAWN"))
	})

	It("includes plan step results from working memory", func() {
		wm.Store("plan_step:FetchDeals:result", "Item A: $5")
		p := prompt("summarize", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Plan Step"))
	})

	It("includes other working memory keys", func() {
		wm.Store("env:region", "us-east-1")
		p := prompt("check region", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Working Memory"))
		Expect(p).To(ContainSubstring("us-east-1"))
	})

	It("includes episodic memory", func() {
		fakeEp.RetrieveWeightedReturns([]memory.Episode{
			{Goal: "similar", Trajectory: "used run_shell", Status: memory.EpisodeSuccess},
		})
		p := prompt("find files", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Relevant Past Experiences"))
	})

	It("truncates large sub-agent results", func() {
		wm.Store("subagent:big:result", strings.Repeat("x", 3000))
		p := prompt("summarize", "", "", 0, nil)
		Expect(p).To(ContainSubstring("omitted for brevity"))
	})

	It("includes partial/error subagent results from working memory", func() {
		wm.Store("subagent:failed-scanner:result", "[TIME LIMIT REACHED] partial data from 2 of 5 repos")
		p := prompt("synthesize findings", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Sub-Agent Results"))
		Expect(p).To(ContainSubstring("failed-scanner"))
		Expect(p).To(ContainSubstring("partial data from 2 of 5 repos"))
	})

	It("includes mixed success and partial subagent results", func() {
		wm.Store("subagent:repo-scanner-1:result", "Found 3 open PRs in repo-A")
		wm.Store("subagent:repo-scanner-2:result", "[BUDGET EXCEEDED] Found 1 PR in repo-B before limit")
		p := prompt("compile report", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Sub-Agent Results"))
		Expect(p).To(ContainSubstring("repo-scanner-1"))
		Expect(p).To(ContainSubstring("repo-scanner-2"))
		Expect(p).To(ContainSubstring("Found 3 open PRs"))
		Expect(p).To(ContainSubstring("BUDGET EXCEEDED"))
	})

	It("includes failed episodic memories so agents learn from past failures", func() {
		fakeEp.RetrieveWeightedReturns([]memory.Episode{
			{Goal: "scan repos for PRs", Trajectory: "[BUDGET EXCEEDED] partial data", Status: memory.EpisodeFailure,
				Reflection: "Budget was exceeded. Use pagination or limit scope."},
		})
		p := prompt("scan repos for PRs", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Relevant Past Experiences"))
		Expect(p).To(ContainSubstring("Previous Failure"))
		Expect(p).To(ContainSubstring("Budget was exceeded"))
	})

	It("shows both successful and failed episodes for context", func() {
		fakeEp.RetrieveWeightedReturns([]memory.Episode{
			{Goal: "check services", Trajectory: "all 5 services healthy", Status: memory.EpisodeSuccess},
			{Goal: "check services", Trajectory: "[TIME LIMIT] only checked 2", Status: memory.EpisodeFailure,
				Reflection: "Time limit reached. Only checked 2 of 5 services."},
		})
		p := prompt("check services", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Relevant Past Experiences"))
		Expect(p).To(ContainSubstring("all 5 services healthy"))
		Expect(p).To(ContainSubstring("Time limit reached"))
	})

	It("includes consolidated wisdom notes when WisdomStore is provided", func() {
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeWs.RetrieveWisdomReturns([]memory.WisdomNote{
			{Summary: "On 2026-03-10, you learned:\n- Always check nil before deref"},
		})
		p := buildAgentPrompt(context.Background(), "find files", wm, fakeEp, fakeWs,
			"", "", 0, nil)
		Expect(p).To(ContainSubstring("Consolidated Lessons"))
		Expect(p).To(ContainSubstring("Always check nil"))
	})

	It("skips wisdom section when WisdomStore returns nothing", func() {
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeWs.RetrieveWisdomReturns(nil)
		p := buildAgentPrompt(context.Background(), "find files", wm, fakeEp, fakeWs,
			"", "", 0, nil)
		Expect(p).NotTo(ContainSubstring("Consolidated Lessons"))
	})
})

var _ = Describe("generateFailureReflection", func() {
	It("returns a basic fallback when reflector is nil", func() {
		result := generateFailureReflection(context.Background(), "deploy app", "connection refused", nil)
		Expect(result).To(ContainSubstring("Task failed with output: connection refused"))
	})

	It("truncates long error output in fallback", func() {
		longError := strings.Repeat("x", 300)
		result := generateFailureReflection(context.Background(), "deploy", longError, nil)
		Expect(result).To(ContainSubstring("..."))
		Expect(len(result)).To(BeNumerically("<", 300))
	})

	It("delegates to the reflector when available", func() {
		fakeReflector := &memoryfakes.FakeFailureReflector{}
		fakeReflector.ReflectReturns("Use retry logic")

		result := generateFailureReflection(context.Background(), "deploy", "timeout", fakeReflector)
		Expect(result).To(Equal("Use retry logic"))
		Expect(fakeReflector.ReflectCallCount()).To(Equal(1))
	})
})

var _ = Describe("scoreEpisodeImportance", func() {
	It("returns 0 when scorer is nil", func() {
		score := scoreEpisodeImportance(context.Background(), nil, "test", "output", memory.EpisodeSuccess)
		Expect(score).To(Equal(0))
	})

	It("delegates to the scorer when available", func() {
		fakeScorer := &memoryfakes.FakeImportanceScorer{}
		fakeScorer.ScoreReturns(7)

		score := scoreEpisodeImportance(context.Background(), fakeScorer, "deploy", "deployed ok", memory.EpisodeSuccess)
		Expect(score).To(Equal(7))
		Expect(fakeScorer.ScoreCallCount()).To(Equal(1))

		_, req := fakeScorer.ScoreArgsForCall(0)
		Expect(req.Goal).To(Equal("deploy"))
		Expect(req.Output).To(Equal("deployed ok"))
		Expect(req.Status).To(Equal(memory.EpisodeSuccess))
	})
})

