package atlassian_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/atlassian"
	"github.com/appcd-dev/genie/pkg/tools/atlassian/atlassianfakes"
)

var _ = Describe("Atlassian Tools", func() {
	var fake *atlassianfakes.FakeService

	BeforeEach(func() {
		fake = new(atlassianfakes.FakeService)
	})

	// ── Jira Tools ──────────────────────────────────────────────────────

	Describe("jira_search_issues", func() {
		It("should return matching issues", func(ctx context.Context) {
			fake.JiraSearchIssuesReturns([]atlassian.IssueSummary{
				{Key: "PROJ-1", Summary: "Fix login bug", Status: "Open", Type: "Bug"},
				{Key: "PROJ-2", Summary: "Add search", Status: "In Progress", Type: "Story"},
			}, nil)

			tool := atlassian.NewJiraSearchTool(fake)
			reqJSON, _ := json.Marshal(struct {
				JQL        string `json:"jql"`
				MaxResults int    `json:"max_results"`
			}{JQL: "project=PROJ", MaxResults: 10})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issues, ok := resp.([]atlassian.IssueSummary)
			Expect(ok).To(BeTrue())
			Expect(issues).To(HaveLen(2))
			Expect(issues[0].Key).To(Equal("PROJ-1"))
			Expect(issues[1].Status).To(Equal("In Progress"))

			Expect(fake.JiraSearchIssuesCallCount()).To(Equal(1))
			_, jql, maxResults := fake.JiraSearchIssuesArgsForCall(0)
			Expect(jql).To(Equal("project=PROJ"))
			Expect(maxResults).To(Equal(10))
		})

		It("should default max_results to 20 when omitted", func(ctx context.Context) {
			fake.JiraSearchIssuesReturns(nil, nil)

			tool := atlassian.NewJiraSearchTool(fake)
			reqJSON, _ := json.Marshal(struct {
				JQL string `json:"jql"`
			}{JQL: "project=X"})

			_, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			_, _, maxResults := fake.JiraSearchIssuesArgsForCall(0)
			Expect(maxResults).To(Equal(20))
		})
	})

	Describe("jira_get_issue", func() {
		It("should return issue detail", func(ctx context.Context) {
			fake.JiraGetIssueReturns(&atlassian.IssueDetail{
				Key:      "PROJ-42",
				Summary:  "Critical bug",
				Status:   "Open",
				Priority: "High",
				Assignee: "alice",
				Labels:   []string{"critical", "backend"},
			}, nil)

			tool := atlassian.NewJiraGetIssueTool(fake)
			reqJSON, _ := json.Marshal(struct {
				IssueKey string `json:"issue_key"`
			}{IssueKey: "PROJ-42"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			detail, ok := resp.(*atlassian.IssueDetail)
			Expect(ok).To(BeTrue())
			Expect(detail.Key).To(Equal("PROJ-42"))
			Expect(detail.Priority).To(Equal("High"))
			Expect(detail.Labels).To(ContainElement("critical"))
		})
	})

	Describe("jira_create_issue", func() {
		It("should create and return new issue", func(ctx context.Context) {
			fake.JiraCreateIssueReturns(&atlassian.IssueSummary{
				Key: "PROJ-99", Summary: "New feature", Type: "Story",
			}, nil)

			tool := atlassian.NewJiraCreateIssueTool(fake)
			reqJSON, _ := json.Marshal(atlassian.CreateIssueInput{
				ProjectKey: "PROJ", Summary: "New feature", IssueType: "Story",
			})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issue, ok := resp.(*atlassian.IssueSummary)
			Expect(ok).To(BeTrue())
			Expect(issue.Key).To(Equal("PROJ-99"))

			_, input := fake.JiraCreateIssueArgsForCall(0)
			Expect(input.ProjectKey).To(Equal("PROJ"))
			Expect(input.IssueType).To(Equal("Story"))
		})
	})

	Describe("jira_update_issue", func() {
		It("should return success message", func(ctx context.Context) {
			fake.JiraUpdateIssueReturns(nil)

			tool := atlassian.NewJiraUpdateIssueTool(fake)
			reqJSON, _ := json.Marshal(struct {
				IssueKey string `json:"issue_key"`
				Summary  string `json:"summary"`
			}{IssueKey: "PROJ-1", Summary: "Updated title"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("PROJ-1"))
			Expect(resp).To(ContainSubstring("updated"))
		})
	})

	Describe("jira_add_comment", func() {
		It("should return success message", func(ctx context.Context) {
			fake.JiraAddCommentReturns(nil)

			tool := atlassian.NewJiraAddCommentTool(fake)
			reqJSON, _ := json.Marshal(struct {
				IssueKey string `json:"issue_key"`
				Body     string `json:"body"`
			}{IssueKey: "PROJ-1", Body: "LGTM"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("PROJ-1"))

			_, key, body := fake.JiraAddCommentArgsForCall(0)
			Expect(key).To(Equal("PROJ-1"))
			Expect(body).To(Equal("LGTM"))
		})
	})

	Describe("jira_list_transitions", func() {
		It("should return transitions", func(ctx context.Context) {
			fake.JiraListTransitionsReturns([]atlassian.Transition{
				{ID: "11", Name: "To Do"},
				{ID: "21", Name: "In Progress"},
				{ID: "31", Name: "Done"},
			}, nil)

			tool := atlassian.NewJiraListTransitionsTool(fake)
			reqJSON, _ := json.Marshal(struct {
				IssueKey string `json:"issue_key"`
			}{IssueKey: "PROJ-1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			transitions, ok := resp.([]atlassian.Transition)
			Expect(ok).To(BeTrue())
			Expect(transitions).To(HaveLen(3))
			Expect(transitions[2].Name).To(Equal("Done"))
		})
	})

	Describe("jira_transition_issue", func() {
		It("should return success message", func(ctx context.Context) {
			fake.JiraTransitionIssueReturns(nil)

			tool := atlassian.NewJiraTransitionIssueTool(fake)
			reqJSON, _ := json.Marshal(struct {
				IssueKey     string `json:"issue_key"`
				TransitionID string `json:"transition_id"`
			}{IssueKey: "PROJ-5", TransitionID: "31"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("PROJ-5"))

			_, key, tid := fake.JiraTransitionIssueArgsForCall(0)
			Expect(key).To(Equal("PROJ-5"))
			Expect(tid).To(Equal("31"))
		})
	})

	// ── Confluence Tools ────────────────────────────────────────────────

	Describe("confluence_search", func() {
		It("should return page summaries", func(ctx context.Context) {
			fake.ConfluenceSearchReturns([]atlassian.PageSummary{
				{ID: "12345", Title: "Architecture", Space: "ENG"},
			}, nil)

			tool := atlassian.NewConfluenceSearchTool(fake)
			reqJSON, _ := json.Marshal(struct {
				CQL string `json:"cql"`
			}{CQL: "space=ENG AND type=page"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			pages, ok := resp.([]atlassian.PageSummary)
			Expect(ok).To(BeTrue())
			Expect(pages).To(HaveLen(1))
			Expect(pages[0].Title).To(Equal("Architecture"))
		})
	})

	Describe("confluence_get_page", func() {
		It("should return page detail", func(ctx context.Context) {
			fake.ConfluenceGetPageReturns(&atlassian.PageDetail{
				ID: "12345", Title: "Architecture", Space: "ENG",
				Body: "<h1>Overview</h1>", Version: 5,
			}, nil)

			tool := atlassian.NewConfluenceGetPageTool(fake)
			reqJSON, _ := json.Marshal(struct {
				PageID string `json:"page_id"`
			}{PageID: "12345"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			page, ok := resp.(*atlassian.PageDetail)
			Expect(ok).To(BeTrue())
			Expect(page.Body).To(ContainSubstring("Overview"))
			Expect(page.Version).To(Equal(5))
		})
	})

	Describe("confluence_create_page", func() {
		It("should return created page summary", func(ctx context.Context) {
			fake.ConfluenceCreatePageReturns(&atlassian.PageSummary{
				ID: "99999", Title: "New Page", Space: "DEV",
			}, nil)

			tool := atlassian.NewConfluenceCreatePageTool(fake)
			reqJSON, _ := json.Marshal(atlassian.CreatePageInput{
				SpaceKey: "DEV", Title: "New Page", Body: "<p>Content</p>",
			})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			page, ok := resp.(*atlassian.PageSummary)
			Expect(ok).To(BeTrue())
			Expect(page.ID).To(Equal("99999"))
		})
	})

	Describe("confluence_update_page", func() {
		It("should return success message", func(ctx context.Context) {
			fake.ConfluenceUpdatePageReturns(nil)

			tool := atlassian.NewConfluenceUpdatePageTool(fake)
			reqJSON, _ := json.Marshal(struct {
				PageID  string `json:"page_id"`
				Title   string `json:"title"`
				Body    string `json:"body"`
				Version int    `json:"version"`
			}{PageID: "12345", Title: "Updated", Body: "<p>New</p>", Version: 5})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("12345"))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 11 tools (7 Jira + 4 Confluence)", func() {
		fake := new(atlassianfakes.FakeService)
		tools := atlassian.AllTools(fake)
		Expect(tools).To(HaveLen(11))
	})
})

var _ = Describe("New", func() {
	It("should return error when base_url is missing", func() {
		_, err := atlassian.New(atlassian.Config{Token: "t", Email: "e"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("base_url"))
	})

	It("should return error when token is missing", func() {
		_, err := atlassian.New(atlassian.Config{BaseURL: "https://x.atlassian.net", Email: "e"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("token"))
	})

	It("should return error when email is missing", func() {
		_, err := atlassian.New(atlassian.Config{BaseURL: "https://x.atlassian.net", Token: "t"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("email"))
	})
})

var _ = Describe("Atlassian Error Paths", func() {
	var fake *atlassianfakes.FakeService

	BeforeEach(func() {
		fake = new(atlassianfakes.FakeService)
	})

	It("should propagate JiraSearchIssues error", func(ctx context.Context) {
		fake.JiraSearchIssuesReturns(nil, fmt.Errorf("API timeout"))
		tool := atlassian.NewJiraSearchTool(fake)
		_, err := tool.Call(ctx, []byte(`{"jql":"project=X"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("API timeout"))
	})

	It("should propagate JiraGetIssue error", func(ctx context.Context) {
		fake.JiraGetIssueReturns(nil, fmt.Errorf("not found"))
		tool := atlassian.NewJiraGetIssueTool(fake)
		_, err := tool.Call(ctx, []byte(`{"issue_key":"PROJ-999"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate JiraCreateIssue error", func(ctx context.Context) {
		fake.JiraCreateIssueReturns(nil, fmt.Errorf("permission denied"))
		tool := atlassian.NewJiraCreateIssueTool(fake)
		reqJSON, _ := json.Marshal(atlassian.CreateIssueInput{ProjectKey: "X", Summary: "Y", IssueType: "Bug"})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate JiraUpdateIssue error", func(ctx context.Context) {
		fake.JiraUpdateIssueReturns(fmt.Errorf("conflict"))
		tool := atlassian.NewJiraUpdateIssueTool(fake)
		_, err := tool.Call(ctx, []byte(`{"issue_key":"PROJ-1","summary":"x"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate JiraAddComment error", func(ctx context.Context) {
		fake.JiraAddCommentReturns(fmt.Errorf("rate limited"))
		tool := atlassian.NewJiraAddCommentTool(fake)
		_, err := tool.Call(ctx, []byte(`{"issue_key":"PROJ-1","body":"hi"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ConfluenceSearch error", func(ctx context.Context) {
		fake.ConfluenceSearchReturns(nil, fmt.Errorf("forbidden"))
		tool := atlassian.NewConfluenceSearchTool(fake)
		_, err := tool.Call(ctx, []byte(`{"cql":"space=X"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ConfluenceGetPage error", func(ctx context.Context) {
		fake.ConfluenceGetPageReturns(nil, fmt.Errorf("not found"))
		tool := atlassian.NewConfluenceGetPageTool(fake)
		_, err := tool.Call(ctx, []byte(`{"page_id":"999"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ConfluenceUpdatePage error", func(ctx context.Context) {
		fake.ConfluenceUpdatePageReturns(fmt.Errorf("version conflict"))
		tool := atlassian.NewConfluenceUpdatePageTool(fake)
		_, err := tool.Call(ctx, []byte(`{"page_id":"1","title":"x","body":"y","version":1}`))
		Expect(err).To(HaveOccurred())
	})
})
