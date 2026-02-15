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
})
