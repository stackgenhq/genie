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

var _ = Describe("PM Tools", func() {
	Describe("NewGetIssueTool", func() {
		It("should return the issue from the underlying service", func(ctx context.Context) {
			mockSvc := &mockService{
				getIssueFunc: func(_ context.Context, id string) (*pm.Issue, error) {
					if id == "PROJ-1" {
						return &pm.Issue{ID: "PROJ-1", Title: "Important Bug"}, nil
					}
					return nil, nil
				},
			}

			tool := pm.NewGetIssueTool(mockSvc)
			reqJSON, _ := json.Marshal(pm.GetIssueRequest{ID: "PROJ-1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issue, ok := resp.(*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issue.Title).To(Equal("Important Bug"))
		})
	})

	Describe("NewCreateIssueTool", func() {
		It("should create an issue via the underlying service", func(ctx context.Context) {
			mockSvc := &mockService{
				createIssueFunc: func(_ context.Context, input pm.IssueInput) (*pm.Issue, error) {
					return &pm.Issue{ID: "PROJ-2", Title: input.Title}, nil
				},
			}

			tool := pm.NewCreateIssueTool(mockSvc)
			reqJSON, _ := json.Marshal(pm.CreateIssueRequest{
				Title:   "New Task",
				Project: "PROJ",
				Type:    "Task",
			})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issue, ok := resp.(*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issue.ID).To(Equal("PROJ-2"))
		})
	})

	Describe("NewAssignIssueTool", func() {
		It("should assign an issue via the underlying service", func(ctx context.Context) {
			mockSvc := &mockService{
				assignIssueFunc: func(_ context.Context, _ string, _ string) error {
					return nil
				},
			}

			tool := pm.NewAssignIssueTool(mockSvc)
			reqJSON, _ := json.Marshal(pm.AssignIssueRequest{ID: "PROJ-1", Assignee: "user1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(Equal("assigned"))
		})
	})

	Describe("New factory", func() {
		It("should return an error for an unsupported provider", func() {
			_, err := pm.New(pm.Config{Provider: "unknown"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported provider"))
		})

		It("should return an error when Jira config is incomplete", func() {
			_, err := pm.New(pm.Config{Provider: "jira"})
			Expect(err).To(HaveOccurred())
		})

		It("should return an error when Linear token is missing", func() {
			_, err := pm.New(pm.Config{Provider: "linear"})
			Expect(err).To(HaveOccurred())
		})

		It("should return an error when Asana token is missing", func() {
			_, err := pm.New(pm.Config{Provider: "asana"})
			Expect(err).To(HaveOccurred())
		})

		It("should create a Jira service with valid config", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"key":    "X-1",
					"fields": map[string]any{"summary": "test", "status": map[string]string{"name": "Open"}},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "jira", APIToken: "t", BaseURL: srv.URL, Email: "a@b.com"})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc).NotTo(BeNil())
		})

		It("should create a Linear service with valid config", func() {
			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t"})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc).NotTo(BeNil())
		})

		It("should create an Asana service with valid config", func() {
			svc, err := pm.New(pm.Config{Provider: "asana", APIToken: "t"})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc).NotTo(BeNil())
		})
	})
})

// mockService is a simple mock for pm.Service used by tool wrapper tests.
type mockService struct {
	getIssueFunc    func(ctx context.Context, id string) (*pm.Issue, error)
	createIssueFunc func(ctx context.Context, input pm.IssueInput) (*pm.Issue, error)
	assignIssueFunc func(ctx context.Context, id string, assignee string) error
}

func (m *mockService) GetIssue(ctx context.Context, id string) (*pm.Issue, error) {
	if m.getIssueFunc != nil {
		return m.getIssueFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockService) CreateIssue(ctx context.Context, input pm.IssueInput) (*pm.Issue, error) {
	if m.createIssueFunc != nil {
		return m.createIssueFunc(ctx, input)
	}
	return nil, nil
}

func (m *mockService) AssignIssue(ctx context.Context, id string, assignee string) error {
	if m.assignIssueFunc != nil {
		return m.assignIssueFunc(ctx, id, assignee)
	}
	return nil
}
