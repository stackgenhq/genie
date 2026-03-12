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
			fake.ListReposToolReturns(scm.ListReposResponse{
				Repositories: []string{"repo1", "repo2"},
			}, nil)

			tool := scm.NewListReposTool(fake)
			reqJSON, _ := json.Marshal(struct{}{})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			typed, ok := resp.(scm.ListReposResponse)
			Expect(ok).To(BeTrue())
			Expect(typed.Repositories).To(Equal([]string{"repo1", "repo2"}))
			Expect(fake.ListReposToolCallCount()).To(Equal(1))
		})
	})

	Describe("NewListPullRequestsTool", func() {
		It("should return open PRs by default", func(ctx context.Context) {
			fake.ListPullRequestsToolReturns([]scm.PullRequestSummary{
				{Number: 1, Title: "Fix bug", Source: "fix-bug", Target: "main", Author: "alice", State: "open"},
				{Number: 2, Title: "Add feature", Source: "feature", Target: "main", Author: "bob", State: "merged"},
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

			Expect(fake.ListPullRequestsToolCallCount()).To(Equal(1))
			_, req := fake.ListPullRequestsToolArgsForCall(0)
			Expect(req.Repo).To(Equal("owner/repo"))
		})

		It("should pass closed filter when state=closed", func(ctx context.Context) {
			fake.ListPullRequestsToolReturns([]scm.PullRequestSummary{
				{Number: 3, Title: "Old PR", State: "closed", Author: "charlie"},
			}, nil)

			tool := scm.NewListPullRequestsTool(fake)
			reqJSON, _ := json.Marshal(scm.ListPullRequestsRequest{Repo: "owner/repo", State: "closed"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			summaries, ok := resp.([]scm.PullRequestSummary)
			Expect(ok).To(BeTrue())
			Expect(summaries).To(HaveLen(1))
			Expect(summaries[0].State).To(Equal("closed"))

			_, req := fake.ListPullRequestsToolArgsForCall(0)
			Expect(req.State).To(Equal("closed"))
		})
	})

	Describe("NewGetPullRequestTool", func() {
		It("should return a single PR by repo and number", func(ctx context.Context) {
			fake.GetPullRequestToolReturns(&go_scm.PullRequest{Number: 123, Title: "Test PR"}, nil)

			tool := scm.NewGetPullRequestTool(fake)
			reqJSON, _ := json.Marshal(scm.GetPullRequestRequest{Repo: "owner/repo", ID: 123})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			pr, ok := resp.(*go_scm.PullRequest)
			Expect(ok).To(BeTrue())
			Expect(pr.Number).To(Equal(123))
			Expect(pr.Title).To(Equal("Test PR"))

			_, req := fake.GetPullRequestToolArgsForCall(0)
			Expect(req.Repo).To(Equal("owner/repo"))
			Expect(req.ID).To(Equal(123))
		})
	})

	Describe("NewCreatePullRequestTool", func() {
		It("should create a PR with the given input", func(ctx context.Context) {
			fake.CreatePullRequestToolReturns(&go_scm.PullRequest{
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

			_, req := fake.CreatePullRequestToolArgsForCall(0)
			Expect(req.Repo).To(Equal("owner/repo"))
			Expect(req.Title).To(Equal("New Feature"))
			Expect(req.Head).To(Equal("feature-branch"))
			Expect(req.Base).To(Equal("main"))
		})
	})

	Describe("NewListPRChangesTool", func() {
		It("should return changed files", func(ctx context.Context) {
			fake.ListPRChangesToolReturns([]scm.ChangeSummary{
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

			_, req := fake.ListPRChangesToolArgsForCall(0)
			Expect(req.Repo).To(Equal("owner/repo"))
			Expect(req.Number).To(Equal(42))
		})
	})

	Describe("NewListPRCommentsTool", func() {
		It("should return comments", func(ctx context.Context) {
			fake.ListPRCommentsToolReturns([]scm.CommentSummary{
				{ID: 1, Body: "LGTM", Author: "reviewer"},
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
			fake.CreatePRCommentToolReturns(&go_scm.Comment{ID: 99, Body: "Nice work!"}, nil)

			tool := scm.NewCreatePRCommentTool(fake)
			reqJSON, _ := json.Marshal(scm.CreatePRCommentRequest{Repo: "owner/repo", Number: 10, Body: "Nice work!"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			comment, ok := resp.(*go_scm.Comment)
			Expect(ok).To(BeTrue())
			Expect(comment.ID).To(Equal(99))

			_, req := fake.CreatePRCommentToolArgsForCall(0)
			Expect(req.Repo).To(Equal("owner/repo"))
			Expect(req.Number).To(Equal(10))
			Expect(req.Body).To(Equal("Nice work!"))
		})
	})

	Describe("NewListPRCommitsTool", func() {
		It("should return commits", func(ctx context.Context) {
			fake.ListPRCommitsToolReturns([]scm.CommitSummary{
				{Sha: "abc123", Message: "fix bug", Author: "dev1"},
				{Sha: "def456", Message: "add tests", Author: "Dev Two"},
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
			fake.MergePRToolReturns("PR #7 merged successfully", nil)

			tool := scm.NewMergePRTool(fake)
			reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 7})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(Equal("PR #7 merged successfully"))

			_, req := fake.MergePRToolArgsForCall(0)
			Expect(req.Repo).To(Equal("owner/repo"))
			Expect(req.Number).To(Equal(7))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 11 tools", func() {
		fake := new(scmfakes.FakeService)
		tools := scm.AllTools(fake)
		Expect(tools).To(HaveLen(11))
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
		fake.ListReposToolReturns(scm.ListReposResponse{}, fmt.Errorf("API error"))
		tool := scm.NewListReposTool(fake)
		_, err := tool.Call(ctx, []byte(`{}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("API error"))
	})

	It("should propagate MergePullRequest error", func(ctx context.Context) {
		fake.MergePRToolReturns("", fmt.Errorf("merge conflict"))
		tool := scm.NewMergePRTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("merge conflict"))
	})

	It("should propagate GetPullRequest error", func(ctx context.Context) {
		fake.GetPullRequestToolReturns(nil, fmt.Errorf("not found"))
		tool := scm.NewGetPullRequestTool(fake)
		reqJSON, _ := json.Marshal(scm.GetPullRequestRequest{Repo: "owner/repo", ID: 999})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListPullRequestChanges error", func(ctx context.Context) {
		fake.ListPRChangesToolReturns(nil, fmt.Errorf("timeout"))
		tool := scm.NewListPRChangesTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListPullRequestComments error", func(ctx context.Context) {
		fake.ListPRCommentsToolReturns(nil, fmt.Errorf("forbidden"))
		tool := scm.NewListPRCommentsTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListPullRequestCommits error", func(ctx context.Context) {
		fake.ListPRCommitsToolReturns(nil, fmt.Errorf("rate limited"))
		tool := scm.NewListPRCommitsTool(fake)
		reqJSON, _ := json.Marshal(scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("NewCommitAndPRTool", func() {
	var fake *scmfakes.FakeService

	BeforeEach(func() {
		fake = new(scmfakes.FakeService)
	})

	It("should commit files and create a PR", func(ctx context.Context) {
		fake.CommitAndPRToolReturns(scm.CommitAndPRResponse{
			CommittedFiles: []string{"file1.go", "file2.go"},
			Branch:         "feature-branch",
			PRNumber:       42,
			PRLink:         "https://github.com/owner/repo/pull/42",
		}, nil)

		tool := scm.NewCommitAndPRTool(fake)
		reqJSON, _ := json.Marshal(scm.CommitAndPRRequest{
			Repo:          "owner/repo",
			Branch:        "feature-branch",
			CommitMessage: "update files",
			Files: []scm.FileChange{
				{Path: "file1.go", Content: "package main"},
				{Path: "file2.go", Content: "package util"},
			},
			CreatePR: true,
			PRTitle:  "My PR",
		})

		resp, err := tool.Call(ctx, reqJSON)
		Expect(err).NotTo(HaveOccurred())

		result, ok := resp.(scm.CommitAndPRResponse)
		Expect(ok).To(BeTrue())
		Expect(result.CommittedFiles).To(Equal([]string{"file1.go", "file2.go"}))
		Expect(result.PRNumber).To(Equal(42))
		Expect(result.PRLink).To(Equal("https://github.com/owner/repo/pull/42"))
	})

	It("should commit files without creating a PR", func(ctx context.Context) {
		fake.CommitAndPRToolReturns(scm.CommitAndPRResponse{
			CommittedFiles: []string{"README.md"},
			Branch:         "docs-update",
		}, nil)

		tool := scm.NewCommitAndPRTool(fake)
		reqJSON, _ := json.Marshal(scm.CommitAndPRRequest{
			Repo:          "owner/repo",
			Branch:        "docs-update",
			CommitMessage: "update docs",
			Files:         []scm.FileChange{{Path: "README.md", Content: "# Hello"}},
		})

		resp, err := tool.Call(ctx, reqJSON)
		Expect(err).NotTo(HaveOccurred())

		result, ok := resp.(scm.CommitAndPRResponse)
		Expect(ok).To(BeTrue())
		Expect(result.CommittedFiles).To(Equal([]string{"README.md"}))
		Expect(result.PRNumber).To(Equal(0))
	})

	It("should propagate errors", func(ctx context.Context) {
		fake.CommitAndPRToolReturns(scm.CommitAndPRResponse{}, fmt.Errorf("at least one file is required"))

		tool := scm.NewCommitAndPRTool(fake)
		reqJSON, _ := json.Marshal(scm.CommitAndPRRequest{
			Repo:          "owner/repo",
			Branch:        "feature",
			CommitMessage: "empty",
		})

		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one file is required"))
	})
})
