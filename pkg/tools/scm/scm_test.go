// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/stackgenhq/genie/pkg/tools/scm"
	"github.com/stackgenhq/genie/pkg/tools/scm/scmfakes"
)

var _ = Describe("SCM Tools", func() {
	var fake *scmfakes.FakeService

	BeforeEach(func() {
		fake = new(scmfakes.FakeService)
	})

	Describe("NewListReposTool", func() {
		It("should return repository names", func(ctx context.Context) {
			fake.ListReposReturns([]*go_scm.Repository{
				{Name: "repo1"},
				{Name: "repo2"},
			}, nil)

			tool := scm.NewListReposTool(fake)
			reqJSON, _ := json.Marshal(struct{}{})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			typed, ok := resp.(scm.ListReposResponse)
			Expect(ok).To(BeTrue())
			Expect(typed.Repositories).To(Equal([]string{"repo1", "repo2"}))
			Expect(fake.ListReposCallCount()).To(Equal(1))
		})
	})

	Describe("NewListPullRequestsTool", func() {
		It("should return open PRs by default", func(ctx context.Context) {
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{
				{Number: 1, Title: "Fix bug", Source: "fix-bug", Target: "main", Author: go_scm.User{Login: "alice"}},
				{Number: 2, Title: "Add feature", Source: "feature", Target: "main", Author: go_scm.User{Login: "bob"}, Merged: true},
			}, nil)

			tool := scm.NewListPullRequestsTool(fake)
			reqJSON, _ := json.Marshal(scm.ListPullRequestsRequest{Repo: "owner/repo"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			summaries, ok := resp.([]scm.PullRequestSummary)
			Expect(ok).To(BeTrue())
			Expect(summaries).To(HaveLen(2))
			Expect(summaries[0].Title).To(Equal("Fix bug"))
			Expect(summaries[0].Author).To(Equal("alice"))
			Expect(summaries[0].State).To(Equal("open"))
			Expect(summaries[1].State).To(Equal("merged"))

			Expect(fake.ListPullRequestsCallCount()).To(Equal(1))
			_, repo, opts := fake.ListPullRequestsArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
			Expect(opts.Open).To(BeTrue())
			Expect(opts.Closed).To(BeFalse())
		})

		It("should pass closed filter when state=closed", func(ctx context.Context) {
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{
				{Number: 3, Title: "Old PR", Closed: true, Author: go_scm.User{Login: "charlie"}},
			}, nil)

			tool := scm.NewListPullRequestsTool(fake)
			reqJSON, _ := json.Marshal(scm.ListPullRequestsRequest{Repo: "owner/repo", State: "closed"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			summaries, ok := resp.([]scm.PullRequestSummary)
			Expect(ok).To(BeTrue())
			Expect(summaries).To(HaveLen(1))
			Expect(summaries[0].State).To(Equal("closed"))

			_, _, opts := fake.ListPullRequestsArgsForCall(0)
			Expect(opts.Open).To(BeFalse())
			Expect(opts.Closed).To(BeTrue())
		})
	})

	Describe("NewGetPullRequestTool", func() {
		It("should return a single PR by repo and number", func(ctx context.Context) {
			fake.GetPullRequestReturns(&go_scm.PullRequest{Number: 123, Title: "Test PR"}, nil)

			tool := scm.NewGetPullRequestTool(fake)
			reqJSON, _ := json.Marshal(scm.GetPullRequestRequest{Repo: "owner/repo", ID: 123})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			pr, ok := resp.(*go_scm.PullRequest)
			Expect(ok).To(BeTrue())
			Expect(pr.Number).To(Equal(123))
			Expect(pr.Title).To(Equal("Test PR"))

			_, repo, id := fake.GetPullRequestArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
			Expect(id).To(Equal(123))
		})
	})

	Describe("NewCreatePullRequestTool", func() {
		It("should create a PR with the given input", func(ctx context.Context) {
			fake.CreatePullRequestReturns(&go_scm.PullRequest{
				Number: 456, Title: "New Feature", Source: "feature-branch", Target: "main",
			}, nil)

			tool := scm.NewCreatePullRequestTool(fake)
			reqJSON, _ := json.Marshal(scm.CreatePullRequestRequest{
				Repo: "owner/repo", Title: "New Feature", Body: "Description", Head: "feature-branch", Base: "main",
			})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			pr, ok := resp.(*go_scm.PullRequest)
			Expect(ok).To(BeTrue())
			Expect(pr.Number).To(Equal(456))

			_, repo, input := fake.CreatePullRequestArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
			Expect(input.Title).To(Equal("New Feature"))
			Expect(input.Source).To(Equal("feature-branch"))
			Expect(input.Target).To(Equal("main"))
		})
	})

	Describe("NewListPRChangesTool", func() {
		It("should return changed files", func(ctx context.Context) {
			fake.ListPullRequestChangesReturns([]*go_scm.Change{
				{Path: "pkg/main.go", Added: true},
				{Path: "README.md"},
			}, nil)

			tool := scm.NewListPRChangesTool(fake)
			reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 42})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			changes, ok := resp.([]scm.ChangeSummary)
			Expect(ok).To(BeTrue())
			Expect(changes).To(HaveLen(2))
			Expect(changes[0].Path).To(Equal("pkg/main.go"))
			Expect(changes[0].Added).To(BeTrue())

			_, repo, number, _ := fake.ListPullRequestChangesArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
			Expect(number).To(Equal(42))
		})
	})

	Describe("NewListPRCommentsTool", func() {
		It("should return comments", func(ctx context.Context) {
			fake.ListPullRequestCommentsReturns([]*go_scm.Comment{
				{ID: 1, Body: "LGTM", Author: go_scm.User{Login: "reviewer"}},
			}, nil)

			tool := scm.NewListPRCommentsTool(fake)
			reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 10})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			comments, ok := resp.([]scm.CommentSummary)
			Expect(ok).To(BeTrue())
			Expect(comments).To(HaveLen(1))
			Expect(comments[0].Body).To(Equal("LGTM"))
			Expect(comments[0].Author).To(Equal("reviewer"))
		})
	})

	Describe("NewCreatePRCommentTool", func() {
		It("should create a comment", func(ctx context.Context) {
			fake.CreatePullRequestCommentReturns(&go_scm.Comment{ID: 99, Body: "Nice work!"}, nil)

			tool := scm.NewCreatePRCommentTool(fake)
			reqJSON, _ := json.Marshal(scm.CreatePRCommentRequest{Repo: "owner/repo", Number: 10, Body: "Nice work!"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			comment, ok := resp.(*go_scm.Comment)
			Expect(ok).To(BeTrue())
			Expect(comment.ID).To(Equal(99))

			_, repo, number, input := fake.CreatePullRequestCommentArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
			Expect(number).To(Equal(10))
			Expect(input.Body).To(Equal("Nice work!"))
		})
	})

	Describe("NewListPRCommitsTool", func() {
		It("should return commits", func(ctx context.Context) {
			fake.ListPullRequestCommitsReturns([]*go_scm.Commit{
				{Sha: "abc123", Message: "fix bug", Author: go_scm.Signature{Login: "dev1"}},
				{Sha: "def456", Message: "add tests", Author: go_scm.Signature{Name: "Dev Two"}},
			}, nil)

			tool := scm.NewListPRCommitsTool(fake)
			reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 5})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			commits, ok := resp.([]scm.CommitSummary)
			Expect(ok).To(BeTrue())
			Expect(commits).To(HaveLen(2))
			Expect(commits[0].Sha).To(Equal("abc123"))
			Expect(commits[0].Author).To(Equal("dev1"))
			Expect(commits[1].Author).To(Equal("Dev Two"))
		})
	})

	Describe("NewMergePRTool", func() {
		It("should merge and return success message", func(ctx context.Context) {
			fake.MergePullRequestReturns(nil)

			tool := scm.NewMergePRTool(fake)
			reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 7})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(Equal("PR #7 merged successfully"))

			_, repo, number := fake.MergePullRequestArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
			Expect(number).To(Equal(7))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 9 tools", func() {
		fake := new(scmfakes.FakeService)
		tools := scm.AllTools(fake)
		Expect(tools).To(HaveLen(9))
	})
})

var _ = Describe("New", func() {
	It("should return error for unsupported provider", func() {
		cfg := scm.Config{Provider: "bitbucket-server"}
		_, err := scm.New(cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported"))
	})

	It("should create a GitHub service", func() {
		cfg := scm.Config{Provider: "github", Token: "test-token"}
		svc, err := scm.New(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc).NotTo(BeNil())
	})

	It("should create a GitLab service", func() {
		cfg := scm.Config{Provider: "gitlab", Token: "test-token"}
		svc, err := scm.New(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc).NotTo(BeNil())
	})

	It("should create a Bitbucket service", func() {
		cfg := scm.Config{Provider: "bitbucket", Token: "test-token"}
		svc, err := scm.New(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc).NotTo(BeNil())
	})
})

var _ = Describe("SCM Tool Error Paths", func() {
	var fake *scmfakes.FakeService

	BeforeEach(func() {
		fake = new(scmfakes.FakeService)
	})

	It("should propagate ListRepos error", func(ctx context.Context) {
		fake.ListReposReturns(nil, fmt.Errorf("API error"))
		tool := scm.NewListReposTool(fake)
		_, err := tool.Call(ctx, []byte(`{}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("API error"))
	})

	It("should propagate MergePullRequest error", func(ctx context.Context) {
		fake.MergePullRequestReturns(fmt.Errorf("merge conflict"))
		tool := scm.NewMergePRTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("merge conflict"))
	})

	It("should propagate GetPullRequest error", func(ctx context.Context) {
		fake.GetPullRequestReturns(nil, fmt.Errorf("not found"))
		tool := scm.NewGetPullRequestTool(fake)
		reqJSON, _ := json.Marshal(scm.GetPullRequestRequest{Repo: "owner/repo", ID: 999})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListPullRequestChanges error", func(ctx context.Context) {
		fake.ListPullRequestChangesReturns(nil, fmt.Errorf("timeout"))
		tool := scm.NewListPRChangesTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListPullRequestComments error", func(ctx context.Context) {
		fake.ListPullRequestCommentsReturns(nil, fmt.Errorf("forbidden"))
		tool := scm.NewListPRCommentsTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListPullRequestCommits error", func(ctx context.Context) {
		fake.ListPullRequestCommitsReturns(nil, fmt.Errorf("rate limited"))
		tool := scm.NewListPRCommitsTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})
})
