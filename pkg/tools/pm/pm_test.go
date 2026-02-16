package pm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/pm/pmfakes"
)

// newFake returns a FakeService that advertises all 12 operations (for tool wrapper tests).
func newFake() *pmfakes.FakeService {
	fake := &pmfakes.FakeService{}
	fake.SupportedReturns([]string{
		"get_issue", "list_issues", "create_issue", "assign_issue",
		"update_issue", "add_comment", "list_comments", "search_issues",
		"list_teams", "list_labels", "add_label", "list_users",
	})
	return fake
}

var _ = Describe("PM Tools", func() {
	Describe("NewGetIssueTool", func() {
		It("should return the issue from the underlying service", func(ctx context.Context) {
			fake := newFake()
			fake.GetIssueReturns(&pm.Issue{
				ID: "PROJ-1", Title: "Fix login", Status: "Open",
			}, nil)

			tool := pm.NewGetIssueTool(fake)
			reqJSON, _ := json.Marshal(pm.GetIssueRequest{ID: "PROJ-1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issue, ok := resp.(*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issue.ID).To(Equal("PROJ-1"))
			Expect(issue.Title).To(Equal("Fix login"))
			Expect(fake.GetIssueCallCount()).To(Equal(1))
		})
	})

	Describe("NewListIssuesTool", func() {
		It("should return a list of issues from the underlying service", func(ctx context.Context) {
			fake := newFake()
			fake.ListIssuesReturns([]*pm.Issue{
				{ID: "PROJ-1", Title: "Fix login", Status: "Open"},
				{ID: "PROJ-2", Title: "Add signup", Status: "In Progress"},
			}, nil)

			tool := pm.NewListIssuesTool(fake)
			reqJSON, _ := json.Marshal(pm.ListIssuesRequest{Status: "open"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issues, ok := resp.([]*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issues).To(HaveLen(2))
			Expect(fake.ListIssuesCallCount()).To(Equal(1))
		})
	})

	Describe("NewCreateIssueTool", func() {
		It("should create an issue via the underlying service", func(ctx context.Context) {
			fake := newFake()
			fake.CreateIssueReturns(&pm.Issue{
				ID: "PROJ-3", Title: "New feature",
			}, nil)

			tool := pm.NewCreateIssueTool(fake)
			reqJSON, _ := json.Marshal(pm.CreateIssueRequest{
				Title: "New feature", Description: "Implement feature X", Project: "PROJ",
			})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issue, ok := resp.(*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issue.ID).To(Equal("PROJ-3"))
			Expect(fake.CreateIssueCallCount()).To(Equal(1))
		})
	})

	Describe("NewAssignIssueTool", func() {
		It("should assign an issue via the underlying service", func(ctx context.Context) {
			fake := newFake()
			fake.AssignIssueReturns(nil)

			tool := pm.NewAssignIssueTool(fake)
			reqJSON, _ := json.Marshal(pm.AssignIssueRequest{ID: "PROJ-1", Assignee: "user-123"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("assigned"))
			Expect(fake.AssignIssueCallCount()).To(Equal(1))
		})
	})

	Describe("NewUpdateIssueTool", func() {
		It("should update an issue via the underlying service", func(ctx context.Context) {
			fake := newFake()
			fake.UpdateIssueReturns(&pm.Issue{
				ID: "PROJ-1", Title: "Updated Title", Status: "In Progress",
			}, nil)

			tool := pm.NewUpdateIssueTool(fake)
			newTitle := "Updated Title"
			reqJSON, _ := json.Marshal(pm.UpdateIssueRequest{ID: "PROJ-1", Title: &newTitle})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issue, ok := resp.(*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issue.Title).To(Equal("Updated Title"))
			Expect(fake.UpdateIssueCallCount()).To(Equal(1))
		})
	})

	Describe("NewAddCommentTool", func() {
		It("should add a comment to an issue", func(ctx context.Context) {
			fake := newFake()
			fake.AddCommentReturns(&pm.Comment{
				ID: "c1", Body: "Looking into this", Author: "bot",
			}, nil)

			tool := pm.NewAddCommentTool(fake)
			reqJSON, _ := json.Marshal(pm.AddCommentRequest{IssueID: "PROJ-1", Body: "Looking into this"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			comment, ok := resp.(*pm.Comment)
			Expect(ok).To(BeTrue())
			Expect(comment.Body).To(Equal("Looking into this"))
			Expect(comment.Author).To(Equal("bot"))
			Expect(fake.AddCommentCallCount()).To(Equal(1))
		})
	})

	Describe("NewListCommentsTool", func() {
		It("should list comments for an issue", func(ctx context.Context) {
			fake := newFake()
			fake.ListCommentsReturns([]*pm.Comment{
				{ID: "c1", Body: "First", Author: "alice"},
				{ID: "c2", Body: "Second", Author: "bob"},
			}, nil)

			tool := pm.NewListCommentsTool(fake)
			reqJSON, _ := json.Marshal(pm.ListCommentsRequest{IssueID: "PROJ-1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			comments, ok := resp.([]*pm.Comment)
			Expect(ok).To(BeTrue())
			Expect(comments).To(HaveLen(2))
			Expect(fake.ListCommentsCallCount()).To(Equal(1))
		})
	})

	Describe("NewSearchIssuesTool", func() {
		It("should search issues", func(ctx context.Context) {
			fake := newFake()
			fake.SearchIssuesReturns([]*pm.Issue{
				{ID: "P-1", Title: "Result for: login bug"},
			}, nil)

			tool := pm.NewSearchIssuesTool(fake)
			reqJSON, _ := json.Marshal(pm.SearchIssuesRequest{Query: "login bug"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			issues, ok := resp.([]*pm.Issue)
			Expect(ok).To(BeTrue())
			Expect(issues).To(HaveLen(1))
			Expect(issues[0].Title).To(ContainSubstring("login bug"))
			Expect(fake.SearchIssuesCallCount()).To(Equal(1))
		})
	})

	Describe("NewListTeamsTool", func() {
		It("should list teams", func(ctx context.Context) {
			fake := newFake()
			fake.ListTeamsReturns([]*pm.Team{
				{ID: "t1", Name: "Engineering", Key: "ENG"},
				{ID: "t2", Name: "Platform", Key: "PLAT"},
			}, nil)

			tool := pm.NewListTeamsTool(fake)
			reqJSON, _ := json.Marshal(pm.ListTeamsRequest{})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			teams, ok := resp.([]*pm.Team)
			Expect(ok).To(BeTrue())
			Expect(teams).To(HaveLen(2))
			Expect(teams[0].Key).To(Equal("ENG"))
			Expect(fake.ListTeamsCallCount()).To(Equal(1))
		})
	})

	Describe("NewListLabelsTool", func() {
		It("should list labels for a team", func(ctx context.Context) {
			fake := newFake()
			fake.ListLabelsReturns([]*pm.Label{
				{ID: "l1", Name: "Bug", Color: "#ff0000"},
			}, nil)

			tool := pm.NewListLabelsTool(fake)
			reqJSON, _ := json.Marshal(pm.ListLabelsRequest{TeamID: "t1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			labels, ok := resp.([]*pm.Label)
			Expect(ok).To(BeTrue())
			Expect(labels).To(HaveLen(1))
			Expect(labels[0].Name).To(Equal("Bug"))
			Expect(fake.ListLabelsCallCount()).To(Equal(1))
		})
	})

	Describe("NewAddLabelTool", func() {
		It("should add a label to an issue", func(ctx context.Context) {
			fake := newFake()
			fake.AddLabelReturns(nil)

			tool := pm.NewAddLabelTool(fake)
			reqJSON, _ := json.Marshal(pm.AddLabelRequest{IssueID: "PROJ-1", LabelID: "l1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("label l1 added"))
			Expect(fake.AddLabelCallCount()).To(Equal(1))
		})
	})

	Describe("NewListUsersTool", func() {
		It("should list users", func(ctx context.Context) {
			fake := newFake()
			fake.ListUsersReturns([]*pm.User{
				{ID: "u1", Name: "Alice", Email: "alice@co.com"},
				{ID: "u2", Name: "Bob", Email: "bob@co.com"},
			}, nil)

			tool := pm.NewListUsersTool(fake)
			reqJSON, _ := json.Marshal(pm.ListUsersRequest{})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			users, ok := resp.([]*pm.User)
			Expect(ok).To(BeTrue())
			Expect(users).To(HaveLen(2))
			Expect(users[0].Name).To(Equal("Alice"))
			Expect(fake.ListUsersCallCount()).To(Equal(1))
		})
	})

	Describe("New factory", func() {
		It("should return an error for an unsupported provider", func() {
			_, err := pm.New(pm.Config{Provider: "unknown"})
			Expect(err).To(HaveOccurred())
		})

		It("should return a service for linear", func() {
			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(svc).NotTo(BeNil())
		})
	})

	Describe("AllTools", func() {
		It("should return tools matching the provider's supported operations", func() {
			fake := newFake()
			tools := pm.AllTools(fake)
			Expect(tools).To(HaveLen(12))
		})

		It("should only return tools that the provider supports", func() {
			fake := &pmfakes.FakeService{}
			fake.SupportedReturns([]string{"get_issue", "list_issues"})
			tools := pm.AllTools(fake)
			Expect(tools).To(HaveLen(2))
		})
	})
})

var _ = Describe("Linear provider", func() {
	Describe("GetIssue", func() {
		It("should fetch an issue by ID", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issue": map[string]any{
							"id": "abc", "title": "Fix login", "description": "desc",
							"state":    map[string]string{"name": "In Progress"},
							"assignee": map[string]string{"name": "Alice"},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{
				Provider: "linear", APIToken: "lin_tok", BaseURL: srv.URL,
			})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.GetIssue(ctx, "abc")
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.Title).To(Equal("Fix login"))
			Expect(issue.Status).To(Equal("In Progress"))
		})
	})

	Describe("ListIssues", func() {
		It("should list issues", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issues": map[string]any{
							"nodes": []map[string]any{
								{"identifier": "LIN-1", "title": "Task 1", "description": "",
									"state": map[string]string{"name": "Todo"}, "assignee": nil},
							},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			issues, err := svc.ListIssues(ctx, pm.IssueFilter{Status: "open"})
			Expect(err).NotTo(HaveOccurred())
			Expect(issues).To(HaveLen(1))
		})
	})

	Describe("CreateIssue", func() {
		It("should create an issue", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issueCreate": map[string]any{
							"success": true,
							"issue":   map[string]any{"id": "new-id", "title": "New"},
						},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			issue, err := svc.CreateIssue(ctx, pm.IssueInput{
				Title: "New", Description: "Desc", Project: "team-1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(issue.ID).To(Equal("new-id"))
		})
	})

	Describe("AssignIssue", func() {
		It("should assign an issue", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"issueUpdate": map[string]any{"success": true},
					},
				})
			}))
			defer srv.Close()

			svc, err := pm.New(pm.Config{Provider: "linear", APIToken: "t", BaseURL: srv.URL})
			Expect(err).NotTo(HaveOccurred())

			err = svc.AssignIssue(ctx, "issue-id", "user-id")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("UpdateIssue", func() {
		It("should update an issue and return the result", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
								{"identifier": "LIN-5", "title": "Login fix", "description": "",
									"state": map[string]string{"name": "Done"}, "assignee": nil},
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
