package scm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/appcd-dev/genie/pkg/tools/scm"
	go_scm "github.com/drone/go-scm/scm"
)

func TestGitHubListRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 1, "full_name": "owner/repo1", "name": "repo1"},
			{"id": 2, "full_name": "owner/repo2", "name": "repo2"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "github",
		Token:    "test-token",
		BaseURL:  ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	repos, err := svc.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("unexpected error listing repos: %v", err)
	}

	if len(repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(repos))
	}
}

func TestGitHubGetPullRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number": 42,
			"title":  "Test PR",
			"body":   "body",
			"head":   map[string]interface{}{"ref": "feature"},
			"base":   map[string]interface{}{"ref": "main"},
			"user":   map[string]interface{}{"login": "user1"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "github",
		Token:    "test-token",
		BaseURL:  ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	pr, err := svc.GetPullRequest(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("unexpected error getting PR: %v", err)
	}

	if pr.Number != 42 {
		t.Errorf("expected PR number 42, got %d", pr.Number)
	}
	if pr.Title != "Test PR" {
		t.Errorf("expected PR title 'Test PR', got %q", pr.Title)
	}
}

func TestGitHubCreatePullRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number": 99,
			"title":  "New Feature",
			"body":   "description",
			"head":   map[string]interface{}{"ref": "feature"},
			"base":   map[string]interface{}{"ref": "main"},
			"user":   map[string]interface{}{"login": "user1"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "github",
		Token:    "test-token",
		BaseURL:  ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	pr, err := svc.CreatePullRequest(context.Background(), "owner/repo", &go_scm.PullRequestInput{
		Title:  "New Feature",
		Body:   "description",
		Source: "feature",
		Target: "main",
	})
	if err != nil {
		t.Fatalf("unexpected error creating PR: %v", err)
	}

	if pr.Number != 99 {
		t.Errorf("expected PR number 99, got %d", pr.Number)
	}
}

func TestGitHubMissingToken(t *testing.T) {
	_, err := scm.New(scm.Config{
		Provider: "github",
		Token:    "",
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}
