// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/driver/github"
	"github.com/stackgenhq/genie/pkg/tools/scm"
)

// newTestWrapper creates a scmWrapper backed by a mock HTTP server.
// The handler map routes URL paths to handler functions.
func newTestWrapper(handlers map[string]http.HandlerFunc) (scm.Service, *httptest.Server) {
	mux := http.NewServeMux()
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}
	srv := httptest.NewServer(mux)
	client, _ := github.New(srv.URL)
	client.Client = srv.Client()

	cfg := scm.Config{Provider: "github", Token: "test-token", BaseURL: srv.URL}
	svc, _ := scm.New(cfg)
	// We can't easily inject the test server URL into the existing New() factory,
	// so we use a helper that builds the service with the test server.
	// For coverage, we test through the public New() + tool constructors on a real wrapper.
	_ = cfg
	_ = svc

	// Build a wrapper using the test client directly via an unexported-friendly trick:
	// We use the Config + New function but override the base URL.
	return nil, srv
}

var _ = Describe("scmWrapper integration", func() {
	var (
		srv *httptest.Server
		svc scm.Service
	)

	AfterEach(func() {
		if srv != nil {
			srv.Close()
		}
	})

	// Helper: create a test service backed by a mock HTTP server.
	setupService := func(handlers map[string]http.HandlerFunc) {
		mux := http.NewServeMux()
		for pattern, handler := range handlers {
			mux.HandleFunc(pattern, handler)
		}
		srv = httptest.NewServer(mux)

		cfg := scm.Config{Provider: "github", Token: "test-token", BaseURL: srv.URL}
		var err error
		svc, err = scm.New(cfg)
		Expect(err).NotTo(HaveOccurred())
	}

	Describe("FindRepo", func() {
		It("should return the repository", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"full_name":      "owner/repo",
						"name":           "repo",
						"default_branch": "main",
						"private":        false,
					})
				},
			})

			repo, err := svc.FindRepo(ctx, "owner/repo")
			Expect(err).NotTo(HaveOccurred())
			Expect(repo).NotTo(BeNil())
			Expect(repo.Name).To(Equal("repo"))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
			})

			_, err := svc.FindRepo(ctx, "owner/repo")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetPullRequest", func() {
		It("should return a PR", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/42": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"number": 42,
						"title":  "My PR",
						"state":  "open",
						"head":   map[string]any{"ref": "feature"},
						"base":   map[string]any{"ref": "main"},
						"user":   map[string]any{"login": "alice"},
					})
				},
			})

			pr, err := svc.GetPullRequest(ctx, "owner/repo", 42)
			Expect(err).NotTo(HaveOccurred())
			Expect(pr.Number).To(Equal(42))
			Expect(pr.Title).To(Equal("My PR"))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/999": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
			})

			_, err := svc.GetPullRequest(ctx, "owner/repo", 999)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListPullRequests", func() {
		It("should return PRs", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{
							"number": 1, "title": "PR 1", "state": "open",
							"head": map[string]any{"ref": "feat"}, "base": map[string]any{"ref": "main"},
							"user": map[string]any{"login": "alice"},
						},
					})
				},
			})

			prs, err := svc.ListPullRequests(ctx, "owner/repo", go_scm.PullRequestListOptions{Page: 1, Size: 50, Open: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(prs).To(HaveLen(1))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				},
			})

			_, err := svc.ListPullRequests(ctx, "owner/repo", go_scm.PullRequestListOptions{})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreatePullRequest", func() {
		It("should create a PR", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls": func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"number": 123, "title": "New PR",
							"head": map[string]any{"ref": "feat"}, "base": map[string]any{"ref": "main"},
							"user": map[string]any{"login": "alice"},
						})
						return
					}
					w.WriteHeader(http.StatusMethodNotAllowed)
				},
			})

			pr, err := svc.CreatePullRequest(ctx, "owner/repo", &go_scm.PullRequestInput{
				Title: "New PR", Source: "feat", Target: "main",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(pr.Number).To(Equal(123))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnprocessableEntity)
				},
			})

			_, err := svc.CreatePullRequest(ctx, "owner/repo", &go_scm.PullRequestInput{Title: "x"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListPullRequestChanges", func() {
		It("should return changes", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/1/files": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"filename": "main.go", "status": "added"},
					})
				},
			})

			changes, err := svc.ListPullRequestChanges(ctx, "owner/repo", 1, go_scm.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(changes).To(HaveLen(1))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/1/files": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				},
			})

			_, err := svc.ListPullRequestChanges(ctx, "owner/repo", 1, go_scm.ListOptions{})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListPullRequestComments", func() {
		It("should return comments", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/issues/1/comments": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"id": 1, "body": "LGTM", "user": map[string]any{"login": "reviewer"}},
					})
				},
			})

			comments, err := svc.ListPullRequestComments(ctx, "owner/repo", 1, go_scm.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(comments).To(HaveLen(1))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/issues/1/comments": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				},
			})

			_, err := svc.ListPullRequestComments(ctx, "owner/repo", 1, go_scm.ListOptions{})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreatePullRequestComment", func() {
		It("should create a comment", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/issues/1/comments": func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"id": 99, "body": "Nice!", "user": map[string]any{"login": "bot"},
						})
						return
					}
					w.WriteHeader(http.StatusOK)
				},
			})

			comment, err := svc.CreatePullRequestComment(ctx, "owner/repo", 1, &go_scm.CommentInput{Body: "Nice!"})
			Expect(err).NotTo(HaveOccurred())
			Expect(comment.ID).To(Equal(99))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/issues/1/comments": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				},
			})

			_, err := svc.CreatePullRequestComment(ctx, "owner/repo", 1, &go_scm.CommentInput{Body: "x"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListPullRequestCommits", func() {
		It("should return commits", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/1/commits": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"sha": "abc123", "commit": map[string]any{
							"message": "fix bug",
							"author":  map[string]any{"name": "Alice", "email": "a@b.c"},
						}, "author": map[string]any{"login": "alice"}},
					})
				},
			})

			commits, err := svc.ListPullRequestCommits(ctx, "owner/repo", 1, go_scm.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(commits).To(HaveLen(1))
			Expect(commits[0].Sha).To(Equal("abc123"))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/1/commits": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				},
			})

			_, err := svc.ListPullRequestCommits(ctx, "owner/repo", 1, go_scm.ListOptions{})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("MergePullRequest", func() {
		It("should merge successfully", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/1/merge": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"merged": true})
				},
			})

			err := svc.MergePullRequest(ctx, "owner/repo", 1)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/pulls/1/merge": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusConflict)
				},
			})

			err := svc.MergePullRequest(ctx, "owner/repo", 1)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetRepoContent", func() {
		It("should return file content", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/contents/README.md": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"name": "README.md", "path": "README.md",
						"sha": "abc", "content": "SGVsbG8=", "encoding": "base64",
					})
				},
			})

			content, err := svc.GetRepoContent(ctx, scm.GetRepoContentRequest{
				Repo: "owner/repo", Path: "README.md", Ref: "main",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(content).NotTo(BeNil())
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/contents/missing.md": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
			})

			_, err := svc.GetRepoContent(ctx, scm.GetRepoContentRequest{
				Repo: "owner/repo", Path: "missing.md", Ref: "main",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Validate", func() {
		It("should succeed when the API is reachable", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/user/repos": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode([]map[string]any{})
				},
			})

			err := svc.Validate(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/user/repos": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				},
			})

			err := svc.Validate(ctx)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("FindBranch", func() {
		It("should find a branch", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/branches/main": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"name":   "main",
						"commit": map[string]any{"sha": "abc123"},
					})
				},
			})

			ref, err := svc.FindBranch(ctx, "owner/repo", "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.Name).To(Equal("main"))
			Expect(ref.Sha).To(Equal("abc123"))
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/branches/nope": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
			})

			_, err := svc.FindBranch(ctx, "owner/repo", "nope")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateBranch", func() {
		It("should create a branch", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/git/refs": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"ref":    "refs/heads/feature",
						"object": map[string]any{"sha": "abc123"},
					})
				},
			})

			err := svc.CreateBranch(ctx, "owner/repo", &go_scm.ReferenceInput{Name: "feature", Sha: "abc123"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should propagate errors", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/git/refs": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnprocessableEntity)
				},
			})

			err := svc.CreateBranch(ctx, "owner/repo", &go_scm.ReferenceInput{Name: "bad", Sha: "xxx"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateOrUpdateFile", func() {
		It("should create a new file", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/contents/new.txt": func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					// PUT = create
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": map[string]any{"sha": "new-sha"},
					})
				},
			})

			err := svc.CreateOrUpdateFile(ctx, "owner/repo", "new.txt", &go_scm.ContentParams{
				Branch: "main", Message: "add file", Data: []byte("hello"),
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update an existing file", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/contents/existing.txt": func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{
							"sha": "old-sha", "content": "b2xk", "encoding": "base64",
						})
						return
					}
					// PUT = update
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"content": map[string]any{"sha": "new-sha"},
					})
				},
			})

			err := svc.CreateOrUpdateFile(ctx, "owner/repo", "existing.txt", &go_scm.ContentParams{
				Branch: "main", Message: "update file", Data: []byte("updated"),
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Tool-facing methods", func() {
		Describe("ListReposTool", func() {
			It("should return formatted repo names with namespace", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/user/repos": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode([]map[string]any{
							{"full_name": "org/repo1", "name": "repo1", "owner": map[string]any{"login": "org"}},
							{"full_name": "user/repo2", "name": "repo2", "owner": map[string]any{"login": "user"}},
						})
					},
				})

				resp, err := svc.ListReposTool(ctx, go_scm.ListOptions{Page: 1, Size: 10})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Repositories).To(HaveLen(2))
			})
		})

		Describe("ListPullRequestsTool", func() {
			It("should list open PRs by default", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode([]map[string]any{
							{
								"number": 1, "title": "PR 1", "state": "open",
								"head": map[string]any{"ref": "feat"}, "base": map[string]any{"ref": "main"},
								"user": map[string]any{"login": "alice"},
							},
						})
					},
				})

				summaries, err := svc.ListPullRequestsTool(ctx, scm.ListPullRequestsRequest{Repo: "owner/repo"})
				Expect(err).NotTo(HaveOccurred())
				Expect(summaries).To(HaveLen(1))
				Expect(summaries[0].Author).To(Equal("alice"))
			})

			It("should list closed PRs when state=closed", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode([]map[string]any{
							{
								"number": 2, "title": "Old PR", "state": "closed",
								"head": map[string]any{"ref": "old"}, "base": map[string]any{"ref": "main"},
								"user": map[string]any{"login": "bob"}, "merged": true,
							},
						})
					},
				})

				summaries, err := svc.ListPullRequestsTool(ctx, scm.ListPullRequestsRequest{Repo: "owner/repo", State: "closed"})
				Expect(err).NotTo(HaveOccurred())
				Expect(summaries).To(HaveLen(1))
			})
		})

		Describe("GetPullRequestTool", func() {
			It("should return a PR", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls/1": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{
							"number": 1, "title": "PR",
							"head": map[string]any{"ref": "f"}, "base": map[string]any{"ref": "m"},
							"user": map[string]any{"login": "a"},
						})
					},
				})

				pr, err := svc.GetPullRequestTool(ctx, scm.GetPullRequestRequest{Repo: "owner/repo", ID: 1})
				Expect(err).NotTo(HaveOccurred())
				Expect(pr.Number).To(Equal(1))
			})
		})

		Describe("CreatePullRequestTool", func() {
			It("should create a PR", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls": func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"number": 10, "title": "New",
							"head": map[string]any{"ref": "f"}, "base": map[string]any{"ref": "m"},
							"user": map[string]any{"login": "a"},
						})
					},
				})

				pr, err := svc.CreatePullRequestTool(ctx, scm.CreatePullRequestRequest{
					Repo: "owner/repo", Title: "New", Head: "f", Base: "m",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(pr.Number).To(Equal(10))
			})
		})

		Describe("ListPRChangesTool", func() {
			It("should return changes", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls/1/files": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode([]map[string]any{
							{"filename": "a.go", "status": "added"},
						})
					},
				})

				changes, err := svc.ListPRChangesTool(ctx, scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
				Expect(err).NotTo(HaveOccurred())
				Expect(changes).To(HaveLen(1))
			})
		})

		Describe("ListPRCommentsTool", func() {
			It("should return comments", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/issues/1/comments": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode([]map[string]any{
							{"id": 1, "body": "ok", "user": map[string]any{"login": "x"}},
						})
					},
				})

				comments, err := svc.ListPRCommentsTool(ctx, scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
				Expect(err).NotTo(HaveOccurred())
				Expect(comments).To(HaveLen(1))
			})
		})

		Describe("CreatePRCommentTool", func() {
			It("should create a comment", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/issues/1/comments": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"id": 55, "body": "hi", "user": map[string]any{"login": "b"},
						})
					},
				})

				c, err := svc.CreatePRCommentTool(ctx, scm.CreatePRCommentRequest{Repo: "owner/repo", Number: 1, Body: "hi"})
				Expect(err).NotTo(HaveOccurred())
				Expect(c.ID).To(Equal(55))
			})
		})

		Describe("ListPRCommitsTool", func() {
			It("should return commits with author fallback", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls/1/commits": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode([]map[string]any{
							{
								"sha":    "abc",
								"commit": map[string]any{"message": "fix", "author": map[string]any{"name": "Alice"}},
								// no top-level "author" → falls back to commit.author.name
							},
						})
					},
				})

				commits, err := svc.ListPRCommitsTool(ctx, scm.PRNumberRequest{Repo: "owner/repo", Number: 1})
				Expect(err).NotTo(HaveOccurred())
				Expect(commits).To(HaveLen(1))
				Expect(commits[0].Author).To(Equal("Alice"))
			})
		})

		Describe("MergePRTool", func() {
			It("should return success message", func(ctx context.Context) {
				setupService(map[string]http.HandlerFunc{
					"/repos/owner/repo/pulls/7/merge": func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{"merged": true})
					},
				})

				msg, err := svc.MergePRTool(ctx, scm.PRNumberRequest{Repo: "owner/repo", Number: 7})
				Expect(err).NotTo(HaveOccurred())
				Expect(msg).To(Equal("PR #7 merged successfully"))
			})
		})
	})

	Describe("CommitAndPRTool", func() {
		It("should reject empty files", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{})

			_, err := svc.CommitAndPRTool(ctx, scm.CommitAndPRRequest{
				Repo: "owner/repo", Branch: "feat", CommitMessage: "x",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one file is required"))
		})

		It("should resolve default branch, create branch, commit files and create PR", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				// FindRepo → resolve default branch
				"/repos/owner/repo": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"name": "repo", "default_branch": "develop",
					})
				},
				// FindBranch — feature branch does not exist, base branch "develop" exists
				"/repos/owner/repo/branches/feat": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
				"/repos/owner/repo/branches/develop": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"name": "develop", "commit": map[string]any{"sha": "base-sha"},
					})
				},
				// CreateBranch
				"/repos/owner/repo/git/refs": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"ref": "refs/heads/feat", "object": map[string]any{"sha": "base-sha"},
					})
				},
				// Contents.Find (new file) + Contents.Create
				"/repos/owner/repo/contents/file1.go": func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"sha": "s1"}})
				},
				// CreatePullRequest
				"/repos/owner/repo/pulls": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"number":   42,
						"html_url": "https://github.com/owner/repo/pull/42",
						"head":     map[string]any{"ref": "feat"},
						"base":     map[string]any{"ref": "develop"},
						"user":     map[string]any{"login": "bot"},
					})
				},
			})

			resp, err := svc.CommitAndPRTool(ctx, scm.CommitAndPRRequest{
				Repo:          "owner/repo",
				Branch:        "feat",
				CommitMessage: "add files",
				Files:         []scm.FileChange{{Path: "file1.go", Content: "package main"}},
				CreatePR:      true,
				PRTitle:       "New PR",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.CommittedFiles).To(Equal([]string{"file1.go"}))
			Expect(resp.Branch).To(Equal("feat"))
			Expect(resp.PRNumber).To(Equal(42))
		})

		It("should commit to existing branch without creating a PR", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				// FindBranch — branch already exists
				"/repos/owner/repo/branches/existing": func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"name": "existing", "commit": map[string]any{"sha": "sha1"},
					})
				},
				// Contents.Find (existing file) + Contents.Update
				"/repos/owner/repo/contents/readme.md": func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{
							"sha": "old-sha", "content": "b2xk", "encoding": "base64",
						})
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"sha": "new-sha"}})
				},
			})

			resp, err := svc.CommitAndPRTool(ctx, scm.CommitAndPRRequest{
				Repo:          "owner/repo",
				Branch:        "existing",
				BaseBranch:    "main",
				CommitMessage: "update readme",
				Files:         []scm.FileChange{{Path: "readme.md", Content: "# Updated"}},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.CommittedFiles).To(Equal([]string{"readme.md"}))
			Expect(resp.PRNumber).To(Equal(0))
		})

		It("should error when default branch cannot be resolved", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
			})

			_, err := svc.CommitAndPRTool(ctx, scm.CommitAndPRRequest{
				Repo:          "owner/repo",
				Branch:        "feat",
				CommitMessage: "x",
				Files:         []scm.FileChange{{Path: "a.go", Content: "c"}},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolve default branch"))
		})

		It("should error when base branch not found for new branch creation", func(ctx context.Context) {
			setupService(map[string]http.HandlerFunc{
				"/repos/owner/repo/branches/feat": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
				"/repos/owner/repo/branches/main": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				},
			})

			_, err := svc.CommitAndPRTool(ctx, scm.CommitAndPRRequest{
				Repo:          "owner/repo",
				Branch:        "feat",
				BaseBranch:    "main",
				CommitMessage: "x",
				Files:         []scm.FileChange{{Path: "a.go", Content: "c"}},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to find base branch"))
		})
	})
})
