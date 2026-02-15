package scm_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/appcd-dev/genie/pkg/tools/scm"
	go_scm "github.com/drone/go-scm/scm"
)

// MockService implements scm.Service for testing
type MockService struct {
	ListReposFunc         func(ctx context.Context) ([]*go_scm.Repository, error)
	GetPullRequestFunc    func(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error)
	CreatePullRequestFunc func(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error)
}

func (m *MockService) ListRepos(ctx context.Context) ([]*go_scm.Repository, error) {
	if m.ListReposFunc != nil {
		return m.ListReposFunc(ctx)
	}
	return nil, nil
}

func (m *MockService) GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error) {
	if m.GetPullRequestFunc != nil {
		return m.GetPullRequestFunc(ctx, repo, id)
	}
	return nil, nil
}

func (m *MockService) CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error) {
	if m.CreatePullRequestFunc != nil {
		return m.CreatePullRequestFunc(ctx, repo, input)
	}
	return nil, nil
}

func TestListReposTool(t *testing.T) {
	mockSvc := &MockService{
		ListReposFunc: func(ctx context.Context) ([]*go_scm.Repository, error) {
			return []*go_scm.Repository{
				{Name: "repo1"},
				{Name: "repo2"},
			}, nil
		},
	}

	tool := scm.NewListReposTool(mockSvc)

	// Marshaling empty struct for request as the tool expects struct{}{} input via JSON
	reqJSON, _ := json.Marshal(struct{}{})

	resp, err := tool.Call(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("unexpected error calling tool: %v", err)
	}

	respTyped, ok := resp.(scm.ListReposResponse)
	if !ok {
		t.Fatalf("expected response type scm.ListReposResponse, got %T", resp)
	}

	expected := []string{"repo1", "repo2"}
	if !reflect.DeepEqual(respTyped.Repositories, expected) {
		t.Errorf("expected repositories %v, got %v", expected, respTyped.Repositories)
	}
}

func TestGetPullRequestTool(t *testing.T) {
	now := time.Now()
	mockSvc := &MockService{
		GetPullRequestFunc: func(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error) {
			if repo == "owner/repo" && id == 123 {
				return &go_scm.PullRequest{
					Number:  123,
					Title:   "Test PR",
					Created: now,
				}, nil
			}
			return nil, nil
		},
	}

	tool := scm.NewGetPullRequestTool(mockSvc)
	req := scm.GetPullRequestRequest{
		Repo: "owner/repo",
		ID:   123,
	}
	reqJSON, _ := json.Marshal(req)

	resp, err := tool.Call(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("unexpected error calling tool: %v", err)
	}

	respTyped, ok := resp.(*go_scm.PullRequest)
	if !ok {
		t.Fatalf("expected response type *scm.PullRequest, got %T", resp)
	}

	if respTyped.Number != 123 || respTyped.Title != "Test PR" {
		t.Errorf("unexpected PR details: %+v", respTyped)
	}
}

func TestCreatePullRequestTool(t *testing.T) {
	mockSvc := &MockService{
		CreatePullRequestFunc: func(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error) {
			if repo == "owner/repo" && input.Title == "New Feature" && input.Source == "feature-branch" && input.Target == "main" {
				return &go_scm.PullRequest{
					Number: 456,
					Title:  input.Title,
					Body:   input.Body,
					Source: input.Source,
					Target: input.Target,
				}, nil
			}
			return nil, nil
		},
	}

	tool := scm.NewCreatePullRequestTool(mockSvc)
	req := scm.CreatePullRequestRequest{
		Repo:  "owner/repo",
		Title: "New Feature",
		Body:  "Description",
		Head:  "feature-branch",
		Base:  "main",
	}
	reqJSON, _ := json.Marshal(req)

	resp, err := tool.Call(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("unexpected error calling tool: %v", err)
	}

	respTyped, ok := resp.(*go_scm.PullRequest)
	if !ok {
		t.Fatalf("expected response type *scm.PullRequest, got %T", resp)
	}

	if respTyped.Number != 456 || respTyped.Title != "New Feature" {
		t.Errorf("unexpected PR details: %+v", respTyped)
	}
}
