// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package learning_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stackgenhq/genie/pkg/audit/auditfakes"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/learning"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/skills"
)

func fakeResponse(content string) expert.Response {
	return expert.Response{
		Choices: []model.Choice{
			{Message: model.Message{Content: content}},
		},
	}
}

func novelJSON() string {
	return `{"should_create": true, "novelty_score": 9, "name": "deploy-k8s-service", "description": "Deploy a K8s service with rolling update", "instructions": "## What it can do\nDeploy services.\n## How\nSteps.\n## What worked\nRolling.\n## What did not work\nNothing."}`
}

func lowNoveltyJSON() string {
	return `{"should_create": false, "novelty_score": 3, "name": "", "description": "", "instructions": ""}`
}

var _ = Describe("NewLearner", func() {
	It("should create a Learner with all dependencies", func() {
		l := learning.NewLearner(
			&expertfakes.FakeExpert{},
			nil,
			&vectorfakes.FakeIStore{},
			&auditfakes.FakeAuditor{},
			learning.DefaultConfig(),
		)
		Expect(l).NotTo(BeNil())
	})
})

var _ = Describe("Learn", func() {
	var (
		ctx       context.Context
		fakeExp   *expertfakes.FakeExpert
		fakeVS    *vectorfakes.FakeIStore
		fakeAudit *auditfakes.FakeAuditor
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeExp = &expertfakes.FakeExpert{}
		fakeVS = &vectorfakes.FakeIStore{}
		fakeAudit = &auditfakes.FakeAuditor{}
	})

	validReq := func() learning.LearnRequest {
		return learning.LearnRequest{
			Goal:      "Deploy my application to K8s with rolling update",
			Output:    "Successfully deployed with zero-downtime rolling update.",
			ToolsUsed: []string{"run_shell", "create_agent"},
		}
	}

	Context("when skill repository is nil", func() {
		It("should skip learning and audit the skip", func() {
			learner := learning.NewLearner(fakeExp, nil, fakeVS, fakeAudit, learning.DefaultConfig())

			err := learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeAudit.LogCallCount()).To(BeNumerically(">=", 1))
			Expect(fakeExp.DoCallCount()).To(Equal(0)) // LLM should not be called.
		})
	})

	Context("when goal is empty", func() {
		It("should skip learning", func() {
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, learning.LearnRequest{
				Goal:   "   ",
				Output: "some output",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeExp.DoCallCount()).To(Equal(0))
		})
	})

	Context("when output is empty", func() {
		It("should skip learning", func() {
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, learning.LearnRequest{
				Goal:   "do something",
				Output: "",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeExp.DoCallCount()).To(Equal(0))
		})
	})

	Context("when LLM call fails", func() {
		It("should return an error and audit the failure", func() {
			fakeExp.DoReturns(expert.Response{}, errors.New("LLM unavailable"))
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("distillation LLM call"))
			// learning_started + learning_failed.
			Expect(fakeAudit.LogCallCount()).To(BeNumerically(">=", 2))
		})
	})

	Context("when LLM returns empty response", func() {
		It("should skip learning", func() {
			fakeExp.DoReturns(fakeResponse(""), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
			// learning_started + learning_skipped.
			Expect(fakeAudit.LogCallCount()).To(BeNumerically(">=", 2))
		})
	})

	Context("when LLM returns invalid JSON", func() {
		It("should skip learning gracefully", func() {
			fakeExp.DoReturns(fakeResponse("this is not json"), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred()) // Non-fatal.
			Expect(fakeAudit.LogCallCount()).To(BeNumerically(">=", 2))
		})
	})

	Context("when novelty score is below threshold", func() {
		It("should skip skill creation", func() {
			fakeExp.DoReturns(fakeResponse(lowNoveltyJSON()), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeVS.UpsertCallCount()).To(Equal(0)) // No vector store write.
		})
	})

	Context("when should_create is false but score is high", func() {
		It("should still skip skill creation", func() {
			fakeExp.DoReturns(fakeResponse(`{"should_create": false, "novelty_score": 9, "name": "x", "description": "x", "instructions": "x"}`), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeVS.UpsertCallCount()).To(Equal(0))
		})
	})

	Context("when task is novel enough", func() {
		It("should create a skill and index in vector store", func() {
			fakeExp.DoReturns(fakeResponse(novelJSON()), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())

			// Vector store should have been called with the skill.
			Expect(fakeVS.UpsertCallCount()).To(Equal(1))
			_, upsertReq := fakeVS.UpsertArgsForCall(0)
			Expect(upsertReq.Items).To(HaveLen(1))
			Expect(upsertReq.Items[0].ID).To(Equal("skill:deploy-k8s-service"))
			Expect(upsertReq.Items[0].Metadata["type"]).To(Equal("learned_skill"))

			// Audit should have: learning_started + skill_created.
			Expect(fakeAudit.LogCallCount()).To(BeNumerically(">=", 2))
		})

		It("should handle markdown-fenced JSON from LLM", func() {
			fenced := "```json\n" + novelJSON() + "\n```"
			fakeExp.DoReturns(fakeResponse(fenced), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
			Expect(fakeVS.UpsertCallCount()).To(Equal(1))
		})
	})

	Context("when vector store is nil", func() {
		It("should still create the skill without error", func() {
			fakeExp.DoReturns(fakeResponse(novelJSON()), nil)
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, nil, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when vector store upsert fails", func() {
		It("should still succeed (non-fatal)", func() {
			fakeExp.DoReturns(fakeResponse(novelJSON()), nil)
			fakeVS.UpsertReturns(errors.New("vector store down"))
			repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
			Expect(err).NotTo(HaveOccurred())
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

			err = learner.Learn(ctx, validReq())

			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("parseProposal (via Learn)", func() {
	It("should parse valid JSON", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}
		fakeExp.DoReturns(fakeResponse(novelJSON()), nil)
		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:   "test goal",
			Output: "test output",
		})

		Expect(err).NotTo(HaveOccurred())
		// If parsed correctly, the skill should be in vector store.
		Expect(fakeVS.UpsertCallCount()).To(Equal(1))
		_, req := fakeVS.UpsertArgsForCall(0)
		Expect(req.Items[0].Metadata["skill_name"]).To(Equal("deploy-k8s-service"))
	})

	It("should handle plain ``` fences", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}
		fenced := "```\n" + novelJSON() + "\n```"
		fakeExp.DoReturns(fakeResponse(fenced), nil)
		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:   "test goal",
			Output: "test output",
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(fakeVS.UpsertCallCount()).To(Equal(1))
	})

	It("should extract JSON embedded in verbose markdown prose", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}
		// Simulate the exact failure scenario: LLM returns markdown analysis with JSON buried inside.
		proseWithJSON := "# Knowledge Distillation Review\n\nSome long analysis...\n\n" + novelJSON() + "\n\n## More analysis..."
		fakeExp.DoReturns(fakeResponse(proseWithJSON), nil)
		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:   "test goal",
			Output: "test output",
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(fakeVS.UpsertCallCount()).To(Equal(1))
		_, req := fakeVS.UpsertArgsForCall(0)
		Expect(req.Items[0].Metadata["skill_name"]).To(Equal("deploy-k8s-service"))
	})
})

var _ = Describe("Retry on parse failure", func() {
	It("should succeed on retry when first response is markdown, second is JSON", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}

		// First call: returns invalid markdown (no JSON inside).
		// Second call (retry): returns valid JSON.
		fakeExp.DoReturnsOnCall(0, fakeResponse("# Analysis\n\nThis task is interesting but no JSON here at all."), nil)
		fakeExp.DoReturnsOnCall(1, fakeResponse(novelJSON()), nil)

		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:   "Deploy my app",
			Output: "Deployed successfully",
		})

		Expect(err).NotTo(HaveOccurred())
		// Expert.Do should have been called twice (original + retry).
		Expect(fakeExp.DoCallCount()).To(Equal(2))
		// Skill should have been created.
		Expect(fakeVS.UpsertCallCount()).To(Equal(1))
	})
})

var _ = Describe("Update-or-create", func() {
	It("should update existing skill when proposal specifies update_existing", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}

		// Return a proposal that says to update an existing skill.
		updateJSON := `{"should_create": true, "novelty_score": 8, "name": "eks-health-v2", "description": "Updated EKS health check", "instructions": "## Updated instructions", "update_existing": "eks-health-check"}`
		fakeExp.DoReturns(fakeResponse(updateJSON), nil)

		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())

		// Create the existing skill first.
		err = repo.Add(skills.AddSkillRequest{
			Name:         "eks-health-check",
			Description:  "Original EKS check",
			Instructions: "## Original instructions",
		})
		Expect(err).NotTo(HaveOccurred())

		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:   "Check EKS cluster health",
			Output: "Health check complete",
		})

		Expect(err).NotTo(HaveOccurred())

		// The existing skill should have been updated (not a new one created).
		Expect(repo.Exists("eks-health-check")).To(BeTrue())
		sk, err := repo.Get("eks-health-check")
		Expect(err).NotTo(HaveOccurred())
		Expect(sk.Body).To(ContainSubstring("Updated instructions"))

		// Vector store should have been re-indexed.
		Expect(fakeVS.UpsertCallCount()).To(Equal(1))
	})
})

var _ = Describe("Vector store audit event", func() {
	It("should emit skill_indexed_in_vector_store audit event on successful index", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}

		fakeExp.DoReturns(fakeResponse(novelJSON()), nil)
		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:      "Deploy via K8s",
			Output:    "Deployed successfully",
			ToolsUsed: []string{"run_shell"},
		})

		Expect(err).NotTo(HaveOccurred())

		// Find the skill_indexed_in_vector_store audit event.
		found := false
		for i := 0; i < fakeAudit.LogCallCount(); i++ {
			_, req := fakeAudit.LogArgsForCall(i)
			if req.Action == "skill_indexed_in_vector_store" {
				found = true
				Expect(req.Metadata["skill_name"]).To(Equal("deploy-k8s-service"))
				break
			}
		}
		Expect(found).To(BeTrue(), "expected skill_indexed_in_vector_store audit event")
	})
})

var _ = Describe("ToolTrace propagation", func() {
	It("should include ToolTrace in distillation prompt when provided", func() {
		fakeExp := &expertfakes.FakeExpert{}
		fakeVS := &vectorfakes.FakeIStore{}
		fakeAudit := &auditfakes.FakeAuditor{}

		fakeExp.DoReturns(fakeResponse(novelJSON()), nil)
		repo, err := skills.NewMutableRepository(GinkgoT().TempDir(), 100)
		Expect(err).NotTo(HaveOccurred())
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit, learning.DefaultConfig())

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:      "Check cluster health",
			Output:    "3 clusters healthy",
			ToolsUsed: []string{"run_shell", "create_agent"},
			ToolTrace: "run_shell: listed clusters → 3 found\ncreate_agent: spawned eks-checker → success",
		})

		Expect(err).NotTo(HaveOccurred())

		// Verify the prompt sent to the expert contains the tool trace.
		_, expertReq := fakeExp.DoArgsForCall(0)
		Expect(expertReq.Message).To(ContainSubstring("run_shell: listed clusters"))
		Expect(expertReq.Message).To(ContainSubstring("Tool Execution Trace"))
	})
})
