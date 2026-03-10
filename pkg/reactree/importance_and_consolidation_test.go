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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/reactree"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("ExpertImportanceScorer", func() {
	var (
		fakeExpert *expertfakes.FakeExpert
		scorer     *reactree.ExpertImportanceScorer
	)

	BeforeEach(func() {
		fakeExpert = &expertfakes.FakeExpert{}
		scorer = reactree.NewExpertImportanceScorer(fakeExpert)
	})

	It("should parse a clean integer response", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "7"}},
			},
		}, nil)

		score := scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "deploy to production",
			Output: "deployed successfully",
			Status: memory.EpisodeSuccess,
		})
		Expect(score).To(Equal(7))
	})

	It("should extract integer from noisy LLM response", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "I would rate this a 8 out of 10."}},
			},
		}, nil)

		score := scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "fix critical bug",
			Output: "patched",
			Status: memory.EpisodeSuccess,
		})
		Expect(score).To(Equal(8))
	})

	It("should clamp scores above 10", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "15"}},
			},
		}, nil)

		score := scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "test",
			Output: "out",
			Status: memory.EpisodeSuccess,
		})
		Expect(score).To(Equal(10))
	})

	It("should return 0 on error", func() {
		fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("rate limited"))

		score := scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "test",
			Output: "out",
			Status: memory.EpisodeSuccess,
		})
		Expect(score).To(Equal(0))
	})

	It("should return 0 for empty choices", func() {
		fakeExpert.DoReturns(expert.Response{Choices: []model.Choice{}}, nil)

		score := scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "test",
			Output: "out",
			Status: memory.EpisodeSuccess,
		})
		Expect(score).To(Equal(0))
	})

	It("should return 0 for unparseable response", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "not a number at all"}},
			},
		}, nil)

		score := scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "test",
			Output: "out",
			Status: memory.EpisodeSuccess,
		})
		Expect(score).To(Equal(0))
	})

	It("should use TaskEfficiency model", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "5"}},
			},
		}, nil)

		_ = scorer.Score(context.Background(), memory.ImportanceScoringRequest{
			Goal:   "test",
			Output: "out",
			Status: memory.EpisodeSuccess,
		})

		_, req := fakeExpert.DoArgsForCall(0)
		Expect(string(req.TaskType)).To(Equal("efficiency"))
		Expect(req.Mode.Silent).To(BeTrue())
	})
})

var _ = Describe("ExpertEpisodeSummarizer", func() {
	var (
		fakeExpert *expertfakes.FakeExpert
		summarizer *reactree.ExpertEpisodeSummarizer
	)

	BeforeEach(func() {
		fakeExpert = &expertfakes.FakeExpert{}
		summarizer = reactree.NewExpertEpisodeSummarizer(fakeExpert)
	})

	It("should summarize episodes into concise wisdom", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "- Always verify health endpoint\n- Use pagination for large lists"}},
			},
		}, nil)

		episodes := []memory.Episode{
			{Goal: "deploy", Trajectory: "deployed", Status: memory.EpisodeSuccess},
			{Goal: "list items", Trajectory: "API timed out", Status: memory.EpisodeFailure, Reflection: "Use pagination"},
		}

		result := summarizer.Summarize(context.Background(), episodes)
		Expect(result).To(ContainSubstring("health endpoint"))
		Expect(result).To(ContainSubstring("pagination"))
	})

	It("should return empty for no episodes", func() {
		result := summarizer.Summarize(context.Background(), nil)
		Expect(result).To(BeEmpty())
	})

	It("should return empty on LLM error", func() {
		fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("model unavailable"))

		episodes := []memory.Episode{
			{Goal: "test", Trajectory: "out", Status: memory.EpisodeSuccess},
		}

		result := summarizer.Summarize(context.Background(), episodes)
		Expect(result).To(BeEmpty())
	})

	It("should truncate long summaries", func() {
		longContent := make([]byte, 2000)
		for i := range longContent {
			longContent[i] = 'A'
		}

		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: string(longContent)}},
			},
		}, nil)

		episodes := []memory.Episode{
			{Goal: "test", Trajectory: "out", Status: memory.EpisodeSuccess},
		}

		result := summarizer.Summarize(context.Background(), episodes)
		Expect(len([]rune(result))).To(BeNumerically("<=", 803)) // 800 + "..."
	})

	It("should prefer reflections over raw trajectory for failures", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "- lesson"}},
			},
		}, nil)

		episodes := []memory.Episode{
			{Goal: "fix bug", Trajectory: "raw error text", Status: memory.EpisodeFailure,
				Reflection: "Check nil pointers"},
		}

		_ = summarizer.Summarize(context.Background(), episodes)

		_, req := fakeExpert.DoArgsForCall(0)
		// The prompt should contain the reflection, not the raw trajectory.
		Expect(req.Message).To(ContainSubstring("Check nil pointer"))
	})
})
