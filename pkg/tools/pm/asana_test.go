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

	Describe("Supported", func() {
		It("should return supported operations", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			supported := svc.Supported()
			Expect(supported).To(ContainElement("get_issue"))
			Expect(supported).To(ContainElement("list_issues"))
			Expect(supported).To(ContainElement("create_issue"))
			Expect(supported).To(ContainElement("assign_issue"))
		})
	})

	Describe("Validate", func() {
		It("should succeed when API returns OK", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/users/me"))
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"data":{"gid":"123"}}`))
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.Validate(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when API returns non-200", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errors":[{"message":"Unauthorized"}]}`))
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "bad-tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			err = svc.Validate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validate failed"))
		})
	})

	Describe("ListIssues", func() {
		It("should list open tasks", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/tasks"))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{
						{"gid": "1", "name": "Task 1", "notes": "notes1", "assignee_status": "upcoming"},
						{"gid": "2", "name": "Task 2", "notes": "notes2", "assignee": map[string]string{"name": "Bob"}},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issues, err := svc.ListIssues(ctx, pm.IssueFilter{})
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(2))
			Expect(issues[0].ID).To(Equal("1"))
			Expect(issues[1].Assignee).To(Equal("Bob"))
		})

		It("should list closed tasks", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.RawQuery).To(ContainSubstring("completed=true"))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "asana", APIToken: "tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.ListIssues(ctx, pm.IssueFilter{Status: "closed"})
			Expect(err).NotTo(HaveOccurred())
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
			svc, err = pm.New(pm.Config{Provider: "asana", APIToken: "tok", BaseURL: srv.URL})
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
