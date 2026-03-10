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
		return buildAgentPrompt(context.Background(), goal, wm, fakeEp,
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
		fakeEp.RetrieveReturns([]memory.Episode{
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
		fakeEp.RetrieveReturns([]memory.Episode{
			{Goal: "scan repos for PRs", Trajectory: "[BUDGET EXCEEDED] partial data", Status: memory.EpisodeFailure},
		})
		p := prompt("scan repos for PRs", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Relevant Past Experiences"))
		Expect(p).To(ContainSubstring("BUDGET EXCEEDED"))
		Expect(p).To(ContainSubstring(string(memory.EpisodeFailure)))
	})

	It("shows both successful and failed episodes for context", func() {
		fakeEp.RetrieveReturns([]memory.Episode{
			{Goal: "check services", Trajectory: "all 5 services healthy", Status: memory.EpisodeSuccess},
			{Goal: "check services", Trajectory: "[TIME LIMIT] only checked 2", Status: memory.EpisodeFailure},
		})
		p := prompt("check services", "", "", 0, nil)
		Expect(p).To(ContainSubstring("Relevant Past Experiences"))
		Expect(p).To(ContainSubstring("all 5 services healthy"))
		Expect(p).To(ContainSubstring("TIME LIMIT"))
	})
})
