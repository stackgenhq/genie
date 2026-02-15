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

var _ = Describe("Asana Provider", func() {
	Describe("GetIssue", func() {
		It("should return the task for a valid GID", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/tasks/12345"))
				Expect(r.Method).To(Equal(http.MethodGet))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"gid":             "12345",
						"name":            "Update docs",
						"notes":           "Documentation is stale",
						"assignee_status": "upcoming",
						"assignee":        map[string]string{"name": "Carol"},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "asana_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.GetIssue(ctx, "12345")
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("12345"))
			Expect(issue.Title).To(Equal("Update docs"))
			Expect(issue.Assignee).To(Equal("Carol"))
			Expect(issue.Description).To(Equal("Documentation is stale"))
		})
	})

	Describe("CreateIssue", func() {
		It("should create a task and return the result", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/tasks"))
				Expect(r.Method).To(Equal(http.MethodPost))
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"gid":  "67890",
						"name": "New task",
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "asana_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.CreateIssue(ctx, pm.IssueInput{
				Title: "New task", Project: "proj-gid",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("67890"))
		})
	})

	Describe("AssignIssue", func() {
		It("should assign a task successfully", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/tasks/12345"))
				Expect(r.Method).To(Equal(http.MethodPut))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{"gid": "12345"},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "asana_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.AssignIssue(ctx, "12345", "user-gid")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the API returns an error", func() {
		It("should parse the Asana error response", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]string{
						{"message": "Not authorized"},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "asana_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.GetIssue(ctx, "12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Not authorized"))
		})
	})

	Context("when token is missing", func() {
		It("should return an error", func() {
			_, err := pm.New(pm.Config{Provider: "asana"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("api_token"))
		})
	})
})
