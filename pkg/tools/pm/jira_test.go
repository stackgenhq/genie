// Copyright (C) 2026 StackGen, Inc. All rights reserved.
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

var _ = Describe("Jira Provider", func() {
	Describe("GetIssue", func() {
		It("should return the issue for a valid ID", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/rest/api/3/issue/PROJ-1"))
				Expect(r.Method).To(Equal(http.MethodGet))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"key": "PROJ-1",
					"fields": map[string]any{
						"summary": "Fix login bug",
						"status":  map[string]string{"name": "In Progress"},
						"assignee": map[string]string{
							"displayName": "Alice",
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira",
				APIToken: "tok",
				BaseURL:  srv.URL,
				Email:    "alice@example.com",
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.GetIssue(ctx, "PROJ-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("PROJ-1"))
			Expect(issue.Title).To(Equal("Fix login bug"))
			Expect(issue.Assignee).To(Equal("Alice"))
			Expect(issue.Status).To(Equal("In Progress"))
		})

		It("should handle ADF description format", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"key": "PROJ-2",
					"fields": map[string]any{
						"summary": "ADF issue",
						"status":  map[string]string{"name": "Open"},
						"description": map[string]any{
							"type":    "doc",
							"version": 1,
							"content": []map[string]any{
								{"type": "paragraph", "content": []map[string]string{
									{"type": "text", "text": "Rich text"},
								}},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.GetIssue(ctx, "PROJ-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.Description).To(ContainSubstring("Rich text"))
		})

		It("should handle nil description", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"key": "PROJ-3",
					"fields": map[string]any{
						"summary": "No desc",
						"status":  map[string]string{"name": "Open"},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.GetIssue(ctx, "PROJ-3")
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.Description).To(BeEmpty())
		})

		It("should return an error for a non-existent issue", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"errorMessages":["Issue not found"]}`))
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.GetIssue(ctx, "NOPE-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HTTP 404"))
		})
	})

	Describe("CreateIssue", func() {
		It("should create an issue and return the key", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/rest/api/3/issue"))
				Expect(r.Method).To(Equal(http.MethodPost))
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]string{"key": "PROJ-42"})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.CreateIssue(ctx, pm.IssueInput{
				Title: "New bug", Project: "PROJ", Type: "Bug",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("PROJ-42"))
		})
	})

	Describe("AssignIssue", func() {
		It("should assign an issue successfully", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/rest/api/3/issue/PROJ-1/assignee"))
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.AssignIssue(ctx, "PROJ-1", "acc123")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when config is incomplete", func() {
		It("should return an error for missing base_url", func() {
			_, err := pm.New(pm.Config{Provider: "jira", APIToken: "t", Email: "a@b.com"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("base_url"))
		})

		It("should return an error for missing email", func() {
			_, err := pm.New(pm.Config{Provider: "jira", APIToken: "t", BaseURL: "http://x"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("email"))
		})

		It("should return an error for missing api_token", func() {
			_, err := pm.New(pm.Config{Provider: "jira"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Supported", func() {
		It("should return supported operations", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.Supported()).To(ContainElement("get_issue"))
		})
	})

	Describe("Validate", func() {
		It("should succeed when API returns OK", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/rest/api/3/myself"))
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"accountId":"123"}`))
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.Validate(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when API returns non-200", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errorMessages":["Unauthorized"]}`))
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "bad", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.Validate(ctx)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListIssues", func() {
		It("should list issues via JQL search", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/rest/api/3/search"))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"issues": []map[string]any{
						{
							"key": "PROJ-1",
							"fields": map[string]any{
								"summary": "Bug fix",
								"status":  map[string]string{"name": "Open"},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com",
			})
			Expect(err).NotTo(HaveOccurred())

			issues, err := svc.ListIssues(ctx, pm.IssueFilter{})
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
			Expect(issues[0].ID).To(Equal("PROJ-1"))
		})
	})

	Describe("unimplemented methods", func() {
		var svc pm.Service

		BeforeEach(func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			DeferCleanup(srv.Close)

			var err error
			svc, err = pm.New(pm.Config{Provider: "jira", APIToken: "tok", BaseURL: srv.URL, Email: "a@b.com"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("UpdateIssue should return not implemented", func(ctx context.Context) {
			_, err := svc.UpdateIssue(ctx, "1", pm.IssueUpdate{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("AddComment should return not implemented", func(ctx context.Context) {
			_, err := svc.AddComment(ctx, "1", "comment")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("ListComments should return not implemented", func(ctx context.Context) {
			_, err := svc.ListComments(ctx, "1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("SearchIssues should return not implemented", func(ctx context.Context) {
			_, err := svc.SearchIssues(ctx, "query")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("ListTeams should return not implemented", func(ctx context.Context) {
			_, err := svc.ListTeams(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("ListLabels should return not implemented", func(ctx context.Context) {
			_, err := svc.ListLabels(ctx, "proj")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("AddLabel should return not implemented", func(ctx context.Context) {
			err := svc.AddLabel(ctx, "1", "label")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})

		It("ListUsers should return not implemented", func(ctx context.Context) {
			_, err := svc.ListUsers(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})
	})
})
