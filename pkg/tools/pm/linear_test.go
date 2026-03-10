// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/pm"
)

var _ = Describe("Linear Provider", func() {
	Describe("GetIssue", func() {
		It("should return the issue for a valid ID", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issue": map[string]any{
							"id":          "abc-123",
							"title":       "Fix search",
							"description": "Search is broken",
							"state":       map[string]string{"name": "In Progress"},
							"assignee":    map[string]string{"name": "Bob"},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.GetIssue(ctx, "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("abc-123"))
			Expect(issue.Title).To(Equal("Fix search"))
			Expect(issue.Assignee).To(Equal("Bob"))
			Expect(issue.Status).To(Equal("In Progress"))
		})
	})

	Describe("ListIssues", func() {
		It("should return open issues", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodPost))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issues": map[string]any{
							"nodes": []map[string]any{
								{
									"identifier":  "LIN-1",
									"title":       "Fix login",
									"description": "Login is broken",
									"state":       map[string]string{"name": "In Progress"},
									"assignee":    map[string]string{"name": "Alice"},
								},
								{
									"identifier":  "LIN-2",
									"title":       "Add search",
									"description": "Need search feature",
									"state":       map[string]string{"name": "Todo"},
									"assignee":    nil,
								},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issues, err := svc.ListIssues(ctx, pm.IssueFilter{})
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(2))
			Expect(issues[0].ID).To(Equal("LIN-1"))
			Expect(issues[0].Title).To(Equal("Fix login"))
			Expect(issues[0].Assignee).To(Equal("Alice"))
			Expect(issues[1].ID).To(Equal("LIN-2"))
			Expect(issues[1].Assignee).To(BeEmpty())
		})
	})

	Describe("CreateIssue", func() {
		It("should create an issue and return the result", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issueCreate": map[string]any{
							"success": true,
							"issue":   map[string]any{"id": "xyz-456", "title": "New feature"},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.CreateIssue(ctx, pm.IssueInput{
				Title: "New feature", Project: "team-id",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("xyz-456"))
		})
	})

	Describe("AssignIssue", func() {
		It("should assign an issue successfully", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issueUpdate": map[string]any{"success": true},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.AssignIssue(ctx, "abc-123", "user-id")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("UpdateIssue", func() {
		It("should update an issue and return the result", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issueUpdate": map[string]any{
							"success": true,
							"issue": map[string]any{
								"identifier": "LIN-1", "title": "New Title", "description": "desc",
								"state":    map[string]string{"name": "In Progress"},
								"assignee": map[string]string{"name": "Alice"},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			newTitle := "New Title"
			issue, err := svc.UpdateIssue(ctx, "LIN-1", pm.IssueUpdate{Title: &newTitle})
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.Title).To(Equal("New Title"))
			Expect(issue.Status).To(Equal("In Progress"))
		})
	})

	Describe("AddComment", func() {
		It("should create a comment", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"commentCreate": map[string]any{
							"success": true,
							"comment": map[string]any{"id": "c1", "body": "LGTM", "user": map[string]string{"name": "Bot"}},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			comment, err := svc.AddComment(ctx, "issue-id", "LGTM")
			Expect(err).NotTo(HaveOccurred())
			Expect(comment.Body).To(Equal("LGTM"))
			Expect(comment.Author).To(Equal("Bot"))
		})
	})

	Describe("ListComments", func() {
		It("should list comments for an issue", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issue": map[string]any{
							"comments": map[string]any{
								"nodes": []map[string]any{
									{"id": "c1", "body": "First", "user": map[string]string{"name": "Alice"}},
									{"id": "c2", "body": "Second", "user": nil},
								},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			comments, err := svc.ListComments(ctx, "issue-id")
			Expect(err).NotTo(HaveOccurred())
			Expect(comments).To(HaveLen(2))
			Expect(comments[0].Author).To(Equal("Alice"))
		})
	})

	Describe("SearchIssues", func() {
		It("should search and return matching issues", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"searchIssues": map[string]any{
							"nodes": []map[string]any{
								{"identifier": "LIN-5", "title": "Login fix", "description": "", "state": map[string]string{"name": "Done"}, "assignee": nil},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			issues, err := svc.SearchIssues(ctx, "login")
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			Expect(issues[0].Title).To(Equal("Login fix"))
		})
	})

	Describe("ListTeams", func() {
		It("should return teams", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"teams": map[string]any{
							"nodes": []map[string]any{
								{"id": "t1", "name": "Engineering", "key": "ENG"},
								{"id": "t2", "name": "Platform", "key": "PLAT"},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			teams, err := svc.ListTeams(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(teams).To(HaveLen(2))
			Expect(teams[0].Key).To(Equal("ENG"))
		})
	})

	Describe("ListLabels", func() {
		It("should return labels for a team", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"team": map[string]any{
							"labels": map[string]any{
								"nodes": []map[string]any{
									{"id": "l1", "name": "Bug", "color": "#ff0000"},
								},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			labels, err := svc.ListLabels(ctx, "t1")
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(HaveLen(1))
			Expect(labels[0].Name).To(Equal("Bug"))
		})
	})

	Describe("AddLabel", func() {
		It("should add a label", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{"issueAddLabel": map[string]any{"success": true}},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			err = svc.AddLabel(ctx, "issue-id", "label-id")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ListUsers", func() {
		It("should return users", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"users": map[string]any{
							"nodes": []map[string]any{
								{"id": "u1", "name": "Alice", "email": "alice@co.com"},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			users, err := svc.ListUsers(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(users).To(HaveLen(1))
			Expect(users[0].Email).To(Equal("alice@co.com"))
		})
	})

	Context("when the GraphQL API returns an error", func() {
		It("should propagate the error message", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]string{
						{"message": "Issue not found"},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.GetIssue(ctx, "nope")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Issue not found"))
		})
	})

	Context("when token is missing", func() {
		It("should return an error", func() {
			_, err := pm.New(pm.Config{Provider: "linear"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("api_token"))
		})
	})
})
