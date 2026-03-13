// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm

import (
	"context"
	"fmt"
	"sync"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/drone/go-scm/scm/traverse"
	"golang.org/x/sync/errgroup"
)

// scmWrapper implements the Service interface by wrapping a go-scm Client.
// It provides a uniform interface for repository and pull-request operations
// across all supported SCM providers (GitHub, GitLab, Bitbucket).
type scmWrapper struct {
	client *go_scm.Client
}

// ── Core Methods ────────────────────────────────────────────────────────

// ListRepos returns all repositories accessible to the authenticated user.
// It paginates through the GitHub API until all pages are consumed.
func (s *scmWrapper) ListRepos(ctx context.Context, request go_scm.ListOptions) ([]*go_scm.Repository, error) {
	repos, err := traverse.ReposV2(ctx, s.client, request)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list repos: %w", err)
	}
	return repos, nil
}

// FindRepo returns a single repository by name (e.g. owner/repo). Used for data ingestion to get metadata (description, link, language).
func (s *scmWrapper) FindRepo(ctx context.Context, repo string) (*go_scm.Repository, error) {
	r, _, err := s.client.Repositories.Find(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to find repo %s: %w", repo, err)
	}
	return r, nil
}

// ListPullRequests returns pull requests for the given repository.
func (s *scmWrapper) ListPullRequests(ctx context.Context, repo string, opts go_scm.PullRequestListOptions) ([]*go_scm.PullRequest, error) {
	prs, _, err := s.client.PullRequests.List(ctx, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PRs in %s: %w", repo, err)
	}
	return prs, nil
}

// GetPullRequest returns a single pull request by repository slug and number.
func (s *scmWrapper) GetPullRequest(ctx context.Context, repo string, id int) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Find(ctx, repo, id)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to get PR %d in %s: %w", id, repo, err)
	}
	return pr, nil
}

// CreatePullRequest opens a new pull request in the given repository.
func (s *scmWrapper) CreatePullRequest(ctx context.Context, repo string, input *go_scm.PullRequestInput) (*go_scm.PullRequest, error) {
	pr, _, err := s.client.PullRequests.Create(ctx, repo, input)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to create PR in %s: %w", repo, err)
	}
	return pr, nil
}

// ListPullRequestChanges returns the files changed in a pull request.
func (s *scmWrapper) ListPullRequestChanges(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Change, error) {
	changes, _, err := s.client.PullRequests.ListChanges(ctx, repo, number, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PR changes for #%d in %s: %w", number, repo, err)
	}
	return changes, nil
}

// ListPullRequestComments returns comments on a pull request.
func (s *scmWrapper) ListPullRequestComments(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Comment, error) {
	comments, _, err := s.client.PullRequests.ListComments(ctx, repo, number, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PR comments for #%d in %s: %w", number, repo, err)
	}
	return comments, nil
}

// CreatePullRequestComment adds a comment to a pull request.
func (s *scmWrapper) CreatePullRequestComment(ctx context.Context, repo string, number int, input *go_scm.CommentInput) (*go_scm.Comment, error) {
	comment, _, err := s.client.PullRequests.CreateComment(ctx, repo, number, input)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to create PR comment on #%d in %s: %w", number, repo, err)
	}
	return comment, nil
}

// ListPullRequestCommits returns the commits in a pull request.
func (s *scmWrapper) ListPullRequestCommits(ctx context.Context, repo string, number int, opts go_scm.ListOptions) ([]*go_scm.Commit, error) {
	commits, _, err := s.client.PullRequests.ListCommits(ctx, repo, number, opts)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to list PR commits for #%d in %s: %w", number, repo, err)
	}
	return commits, nil
}

// MergePullRequest merges a pull request.
func (s *scmWrapper) MergePullRequest(ctx context.Context, repo string, number int) error {
	_, err := s.client.PullRequests.Merge(ctx, repo, number)
	if err != nil {
		return fmt.Errorf("scm: failed to merge PR #%d in %s: %w", number, repo, err)
	}
	return nil
}

func (s *scmWrapper) GetRepoContent(ctx context.Context, req GetRepoContentRequest) (*go_scm.Content, error) {
	content, _, err := s.client.Contents.Find(ctx, req.Repo, req.Path, req.Ref)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to get repo content for %s in %s: %w", req.Path, req.Repo, err)
	}
	return content, nil
}

// Validate performs a lightweight health check by listing repos (page 1, size 1)
// to verify the token and endpoint are valid.
func (s *scmWrapper) Validate(ctx context.Context) error {
	_, _, err := s.client.Repositories.List(ctx, go_scm.ListOptions{Page: 1, Size: 1})
	if err != nil {
		return fmt.Errorf("scm: validate failed: %w", err)
	}
	return nil
}

// CreateOrUpdateFile creates a file if it does not exist, or updates it if it does.
// It auto-detects by first trying Contents.Find to get the current SHA.
func (s *scmWrapper) CreateOrUpdateFile(ctx context.Context, repo, path string, params *go_scm.ContentParams) error {
	existing, _, _ := s.client.Contents.Find(ctx, repo, path, params.Branch)
	if existing != nil {
		params.Sha = existing.Sha
		params.BlobID = existing.BlobID
		_, err := s.client.Contents.Update(ctx, repo, path, params)
		if err != nil {
			return fmt.Errorf("scm: failed to update file %s: %w", path, err)
		}
		return nil
	}
	_, err := s.client.Contents.Create(ctx, repo, path, params)
	if err != nil {
		return fmt.Errorf("scm: failed to create file %s: %w", path, err)
	}
	return nil
}

// FindBranch finds a branch by name, returning nil if not found.
func (s *scmWrapper) FindBranch(ctx context.Context, repo, name string) (*go_scm.Reference, error) {
	ref, _, err := s.client.Git.FindBranch(ctx, repo, name)
	if err != nil {
		return nil, fmt.Errorf("scm: failed to find branch %s: %w", name, err)
	}
	return ref, nil
}

// CreateBranch creates a new branch from the given SHA.
func (s *scmWrapper) CreateBranch(ctx context.Context, repo string, params *go_scm.ReferenceInput) error {
	_, err := s.client.Git.CreateBranch(ctx, repo, params)
	if err != nil {
		return fmt.Errorf("scm: failed to create branch %s: %w", params.Name, err)
	}
	return nil
}

// ── Tool-Facing Methods ─────────────────────────────────────────────────
// These methods accept a single request struct and return tool-friendly
// responses so that each NewXTool constructor can simply pass the method
// reference to NewFunctionTool.

func (s *scmWrapper) ListReposTool(ctx context.Context, req go_scm.ListOptions) (ListReposResponse, error) {
	repos, err := s.ListRepos(ctx, req)
	if err != nil {
		return ListReposResponse{}, err
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		if r.Namespace != "" {
			names[i] = r.Namespace + "/" + r.Name
		} else {
			names[i] = r.Name
		}
	}
	return ListReposResponse{Repositories: names}, nil
}

func (s *scmWrapper) ListPullRequestsTool(ctx context.Context, req ListPullRequestsRequest) ([]PullRequestSummary, error) {
	opts := go_scm.PullRequestListOptions{
		Page:   1,
		Size:   50,
		Open:   true,
		Closed: false,
	}
	if req.State == "closed" {
		opts.Open = false
		opts.Closed = true
	}

	prs, err := s.ListPullRequests(ctx, req.Repo, opts)
	if err != nil {
		return nil, err
	}

	summaries := make([]PullRequestSummary, 0, len(prs))
	for _, pr := range prs {
		author := ""
		if pr.Author.Login != "" {
			author = pr.Author.Login
		}
		state := "open"
		if pr.Closed {
			state = "closed"
		}
		if pr.Merged {
			state = "merged"
		}
		summaries = append(summaries, PullRequestSummary{
			Number: pr.Number,
			Title:  pr.Title,
			State:  state,
			Source: pr.Source,
			Target: pr.Target,
			Author: author,
		})
	}
	return summaries, nil
}

func (s *scmWrapper) GetPullRequestTool(ctx context.Context, req GetPullRequestRequest) (*go_scm.PullRequest, error) {
	return s.GetPullRequest(ctx, req.Repo, req.ID)
}

func (s *scmWrapper) CreatePullRequestTool(ctx context.Context, req CreatePullRequestRequest) (*go_scm.PullRequest, error) {
	input := &go_scm.PullRequestInput{
		Title:  req.Title,
		Body:   req.Body,
		Source: req.Head,
		Target: req.Base,
	}
	return s.CreatePullRequest(ctx, req.Repo, input)
}

func (s *scmWrapper) ListPRChangesTool(ctx context.Context, req PRNumberRequest) ([]ChangeSummary, error) {
	changes, err := s.ListPullRequestChanges(ctx, req.Repo, req.Number, go_scm.ListOptions{Page: 1, Size: 100})
	if err != nil {
		return nil, err
	}
	out := make([]ChangeSummary, 0, len(changes))
	for _, c := range changes {
		out = append(out, ChangeSummary{
			Path:    c.Path,
			Added:   c.Added,
			Deleted: c.Deleted,
			Renamed: c.Renamed,
		})
	}
	return out, nil
}

func (s *scmWrapper) ListPRCommentsTool(ctx context.Context, req PRNumberRequest) ([]CommentSummary, error) {
	comments, err := s.ListPullRequestComments(ctx, req.Repo, req.Number, go_scm.ListOptions{Page: 1, Size: 100})
	if err != nil {
		return nil, err
	}
	out := make([]CommentSummary, 0, len(comments))
	for _, c := range comments {
		out = append(out, CommentSummary{
			ID:     c.ID,
			Body:   c.Body,
			Author: c.Author.Login,
		})
	}
	return out, nil
}

func (s *scmWrapper) CreatePRCommentTool(ctx context.Context, req CreatePRCommentRequest) (*go_scm.Comment, error) {
	return s.CreatePullRequestComment(ctx, req.Repo, req.Number, &go_scm.CommentInput{Body: req.Body})
}

func (s *scmWrapper) ListPRCommitsTool(ctx context.Context, req PRNumberRequest) ([]CommitSummary, error) {
	commits, err := s.ListPullRequestCommits(ctx, req.Repo, req.Number, go_scm.ListOptions{Page: 1, Size: 100})
	if err != nil {
		return nil, err
	}
	out := make([]CommitSummary, 0, len(commits))
	for _, c := range commits {
		author := c.Author.Login
		if author == "" {
			author = c.Author.Name
		}
		out = append(out, CommitSummary{
			Sha:     c.Sha,
			Message: c.Message,
			Author:  author,
		})
	}
	return out, nil
}

func (s *scmWrapper) MergePRTool(ctx context.Context, req PRNumberRequest) (string, error) {
	err := s.MergePullRequest(ctx, req.Repo, req.Number)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("PR #%d merged successfully", req.Number), nil
}

func (s *scmWrapper) CommitAndPRTool(ctx context.Context, req CommitAndPRRequest) (CommitAndPRResponse, error) {
	if len(req.Files) == 0 {
		return CommitAndPRResponse{}, fmt.Errorf("at least one file is required")
	}

	// Resolve the base branch: use the repo's default branch when not specified.
	baseBranch := req.BaseBranch
	if baseBranch == "" {
		repo, err := s.FindRepo(ctx, req.Repo)
		if err != nil {
			return CommitAndPRResponse{}, fmt.Errorf("failed to resolve default branch: %w", err)
		}
		baseBranch = repo.Branch
		if baseBranch == "" {
			baseBranch = "main"
		}
	}

	// Create the target branch if it doesn't already exist.
	_, err := s.FindBranch(ctx, req.Repo, req.Branch)
	if err != nil {
		// Branch not found — create it from the base branch.
		baseRef, findErr := s.FindBranch(ctx, req.Repo, baseBranch)
		if findErr != nil {
			return CommitAndPRResponse{}, fmt.Errorf("failed to find base branch %s: %w", baseBranch, findErr)
		}
		if createErr := s.CreateBranch(ctx, req.Repo, &go_scm.ReferenceInput{
			Name: req.Branch,
			Sha:  baseRef.Sha,
		}); createErr != nil {
			return CommitAndPRResponse{}, fmt.Errorf("failed to create branch %s: %w", req.Branch, createErr)
		}
	}

	// Concurrently look up existing file SHAs so we know which files to
	// create vs update.  The actual writes are sequential because each
	// commit advances the branch HEAD.
	type fileMeta struct {
		sha    string
		blobID string
	}
	shaMap := make(map[string]fileMeta, len(req.Files))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for _, f := range req.Files {
		f := f
		g.Go(func() error {
			existing, _, _ := s.client.Contents.Find(gctx, req.Repo, f.Path, req.Branch)
			if existing != nil {
				mu.Lock()
				shaMap[f.Path] = fileMeta{sha: existing.Sha, blobID: existing.BlobID}
				mu.Unlock()
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return CommitAndPRResponse{}, fmt.Errorf("failed to look up existing files: %w", err)
	}

	// Sequentially commit each file (each creates a separate commit).
	committed := make([]string, 0, len(req.Files))
	for _, f := range req.Files {
		params := &go_scm.ContentParams{
			Branch:  req.Branch,
			Message: req.CommitMessage,
			Data:    []byte(f.Content),
		}

		meta, exists := shaMap[f.Path]
		if exists {
			params.Sha = meta.sha
			params.BlobID = meta.blobID
			_, updateErr := s.client.Contents.Update(ctx, req.Repo, f.Path, params)
			if updateErr != nil {
				return CommitAndPRResponse{}, fmt.Errorf("failed to update file %s: %w", f.Path, updateErr)
			}
		} else {
			_, createErr := s.client.Contents.Create(ctx, req.Repo, f.Path, params)
			if createErr != nil {
				return CommitAndPRResponse{}, fmt.Errorf("failed to create file %s: %w", f.Path, createErr)
			}
		}
		committed = append(committed, f.Path)
	}

	result := CommitAndPRResponse{
		CommittedFiles: committed,
		Branch:         req.Branch,
	}

	// Optionally create a Pull Request.
	if !req.CreatePR {
		return result, nil
	}

	pr, prErr := s.CreatePullRequest(ctx, req.Repo, &go_scm.PullRequestInput{
		Title:  req.PRTitle,
		Body:   req.PRBody,
		Source: req.Branch,
		Target: baseBranch,
	})
	if prErr != nil {
		return CommitAndPRResponse{}, fmt.Errorf("files committed but PR creation failed: %w", prErr)
	}
	result.PRNumber = pr.Number
	result.PRLink = pr.Link
	return result, nil
}
