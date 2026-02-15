package pm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/pm"
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
