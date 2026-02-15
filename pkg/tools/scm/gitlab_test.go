package scm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/appcd-dev/genie/pkg/tools/scm"
)

func TestGitLabListRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 1, "path_with_namespace": "group/repo1", "name": "repo1"},
			{"id": 2, "path_with_namespace": "group/repo2", "name": "repo2"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "gitlab",
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

func TestGitLabGetPullRequest(t *testing.T) {
	mux := http.NewServeMux()
	// GitLab URL-encodes the project path (group%2Frepo)
	mux.HandleFunc("/api/v4/projects/group%2Frepo/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":           10,
			"title":         "MR Title",
			"description":   "body",
			"source_branch": "feature",
			"target_branch": "main",
			"author":        map[string]interface{}{"username": "user1"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "gitlab",
		Token:    "test-token",
		BaseURL:  ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	pr, err := svc.GetPullRequest(context.Background(), "group/repo", 10)
	if err != nil {
		t.Fatalf("unexpected error getting MR: %v", err)
	}

	if pr.Number != 10 {
		t.Errorf("expected MR number 10, got %d", pr.Number)
	}
	if pr.Title != "MR Title" {
		t.Errorf("expected MR title 'MR Title', got %q", pr.Title)
	}
}

func TestGitLabMissingToken(t *testing.T) {
	_, err := scm.New(scm.Config{
		Provider: "gitlab",
		Token:    "",
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}
