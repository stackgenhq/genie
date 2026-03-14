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
			learner := learning.NewLearner(fakeExp, nil, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, nil, fakeAudit)

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
			learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

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
		learner := learning.NewLearner(fakeExp, repo, fakeVS, fakeAudit)

		err = learner.Learn(context.Background(), learning.LearnRequest{
			Goal:   "test goal",
			Output: "test output",
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(fakeVS.UpsertCallCount()).To(Equal(1))
	})
})
