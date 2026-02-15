package scm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/appcd-dev/genie/pkg/tools/scm"
)

func TestBitbucketListRepos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"values": []map[string]interface{}{
				{"uuid": "{1}", "full_name": "team/repo1", "name": "repo1"},
				{"uuid": "{2}", "full_name": "team/repo2", "name": "repo2"},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "bitbucket",
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

func TestBitbucketGetPullRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2.0/repositories/team/repo/pullrequests/5", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          5,
			"title":       "BB PR",
			"description": "body",
			"source":      map[string]interface{}{"branch": map[string]interface{}{"name": "feature"}},
			"destination": map[string]interface{}{"branch": map[string]interface{}{"name": "main"}},
			"author":      map[string]interface{}{"display_name": "user1"},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc, err := scm.New(scm.Config{
		Provider: "bitbucket",
		Token:    "test-token",
		BaseURL:  ts.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	pr, err := svc.GetPullRequest(context.Background(), "team/repo", 5)
	if err != nil {
		t.Fatalf("unexpected error getting PR: %v", err)
	}

	if pr.Number != 5 {
		t.Errorf("expected PR number 5, got %d", pr.Number)
	}
	if pr.Title != "BB PR" {
		t.Errorf("expected PR title 'BB PR', got %q", pr.Title)
	}
}

func TestBitbucketMissingToken(t *testing.T) {
	_, err := scm.New(scm.Config{
		Provider: "bitbucket",
		Token:    "",
	})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}
