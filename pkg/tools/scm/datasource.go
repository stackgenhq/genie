// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package scm provides a single DataSource connector backed by go-scm for all
// SCM providers (GitHub, GitLab, Bitbucket). It lists pull requests, issues,
// repo metadata and recent commit authors for each repo in scope and returns
// them as NormalizedItems for vectorization. One adapter serves all providers.
package scm

import (
	"context"
	"fmt"
	"strings"
	"sync"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/logger"
	"golang.org/x/sync/errgroup"
)

// defaultRecentCommitCount is the number of head-branch commits inspected for
// recent authors when no explicit limit is provided.
const defaultRecentCommitCount = 50

// SCMConnector implements datasource.DataSource for any go-scm provider (GitHub,
// GitLab, Bitbucket). It is parameterized by sourceName so one implementation
// serves all; scope.Get(sourceName) supplies the repo list per provider.
type SCMConnector struct {
	svc               Service
	sourceName        string
	recentCommitCount int
}

// NewSCMConnector returns a DataSource that uses the go-scm Service to list
// repo metadata, pull requests, issues and recent authors. The datasource
// identifier (e.g. "github", "gitlab") is derived from svc.Provider(), and
// scope.Get(sourceName) defines which repos to include.
func NewSCMConnector(svc Service) *SCMConnector {
	return &SCMConnector{
		svc:               svc,
		sourceName:        svc.Provider(),
		recentCommitCount: defaultRecentCommitCount,
	}
}

// NewSCMConnectorWithOptions returns an SCMConnector where callers can override
// the number of recent commits inspected for author extraction.
func NewSCMConnectorWithOptions(svc Service, recentCommitCount int) *SCMConnector {
	if recentCommitCount <= 0 {
		recentCommitCount = defaultRecentCommitCount
	}
	return &SCMConnector{
		svc:               svc,
		sourceName:        svc.Provider(),
		recentCommitCount: recentCommitCount,
	}
}

// Name returns the source identifier (e.g. "github", "gitlab").
func (c *SCMConnector) Name() string {
	return c.sourceName
}

// ListItems lists repo metadata, pull requests, issues and recent commit authors
// for each repo in scope. All four data types are fetched concurrently per repo.
// Errors inside individual fetchers are logged and treated as partial failures
// so that one unavailable resource does not block the rest.
func (c *SCMConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	repos := scope.Get(c.sourceName)
	if len(repos) == 0 {
		return nil, nil
	}

	var (
		mu  sync.Mutex
		out []datasource.NormalizedItem
	)

	g, gctx := errgroup.WithContext(ctx)
	for _, repo := range repos {
		repo := strings.TrimSpace(repo)
		if repo == "" {
			continue
		}

		g.Go(func() error {
			items := c.fetchAllItemsForRepo(gctx, repo)
			if len(items) == 0 {
				return nil
			}
			mu.Lock()
			out = append(out, items...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return out, nil
}

// fetchAllItemsForRepo runs the four sub-fetchers concurrently for a single
// repo and merges their results. Errors are logged inside each fetcher; this
// function always returns whatever was collected successfully.
func (c *SCMConnector) fetchAllItemsForRepo(ctx context.Context, repo string) []datasource.NormalizedItem {
	type result struct {
		items []datasource.NormalizedItem
	}

	results := make([]result, 4)
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		item, ok := c.fetchRepoItem(ctx, repo)
		if ok {
			results[0] = result{items: []datasource.NormalizedItem{item}}
		}
	}()
	go func() {
		defer wg.Done()
		results[1] = result{items: c.fetchPRItems(ctx, repo)}
	}()
	go func() {
		defer wg.Done()
		results[2] = result{items: c.fetchIssueItems(ctx, repo)}
	}()
	go func() {
		defer wg.Done()
		item, ok := c.fetchRecentAuthors(ctx, repo)
		if ok {
			results[3] = result{items: []datasource.NormalizedItem{item}}
		}
	}()

	wg.Wait()

	var out []datasource.NormalizedItem
	for _, r := range results {
		out = append(out, r.items...)
	}
	return out
}

// fetchRepoItem retrieves repo metadata (name, description, URL, languages) and
// returns a NormalizedItem for it. The boolean return indicates whether a valid
// item was produced; on error the failure is silently ignored to match the
// behaviour of the original implementation.
func (c *SCMConnector) fetchRepoItem(ctx context.Context, repo string) (datasource.NormalizedItem, bool) {
	r, err := c.svc.FindRepo(ctx, repo)
	if err != nil || r == nil {
		return datasource.NormalizedItem{}, false
	}

	content := repo
	if r.Description != "" {
		content = repo + "\n\n" + r.Description
	}
	if r.Link != "" {
		content = content + "\n" + r.Link
	}

	meta := map[string]string{"type": "repo", "name": repo}
	if r.Link != "" {
		meta["url"] = r.Link
	}
	if len(r.Language) > 0 {
		langs := make([]string, 0, len(r.Language))
		for lang := range r.Language {
			langs = append(langs, lang)
		}
		meta["language"] = strings.Join(langs, ",")
	}

	updatedAt := r.Updated
	if updatedAt.IsZero() {
		updatedAt = r.Created
	}

	var sourceRef *datasource.SourceRef
	if r.Link != "" {
		sourceRef = &datasource.SourceRef{
			Type:  c.sourceName,
			RefID: r.Link,
		}
	}

	return datasource.NormalizedItem{
		ID:        fmt.Sprintf("%s:repo:%s", c.sourceName, repo),
		Source:    c.sourceName,
		SourceRef: sourceRef,
		UpdatedAt: updatedAt,
		Content:   content,
		Metadata:  meta,
	}, true
}

// fetchPRItems retrieves all open pull requests for the repo (paginated, max
// 100 pages) and converts each one to a NormalizedItem. Errors are logged and
// an empty slice is returned to keep the sync partial rather than fatal.
func (c *SCMConnector) fetchPRItems(ctx context.Context, repo string) []datasource.NormalizedItem {
	var prs []*go_scm.PullRequest
	opts := go_scm.PullRequestListOptions{Open: true, Size: 100}
	for page := 1; page <= 100; page++ {
		opts.Page = page
		pagePRs, err := c.svc.ListPullRequests(ctx, repo, opts)
		if err != nil {
			logger.GetLogger(ctx).Warn("SCM datasource: list pull requests failed, skipping repo",
				"source", c.sourceName, "repo", repo, "error", err)
			return nil
		}
		if len(pagePRs) == 0 {
			break
		}
		prs = append(prs, pagePRs...)
		if len(pagePRs) < opts.Size {
			break
		}
	}

	items := make([]datasource.NormalizedItem, 0, len(prs))
	for _, pr := range prs {
		if pr == nil {
			continue
		}
		content := pr.Title
		if pr.Body != "" {
			content = pr.Title + "\n\n" + pr.Body
		}
		updatedAt := pr.Updated
		if updatedAt.IsZero() {
			updatedAt = pr.Created
		}
		stateStr := "open"
		if pr.Closed {
			stateStr = "closed"
		}
		meta := map[string]string{"type": "pr", "title": pr.Title, "state": stateStr, "repo": repo}
		if pr.Author.Login != "" {
			meta["author"] = pr.Author.Login
		}

		var sourceRef *datasource.SourceRef
		if pr.Link != "" {
			sourceRef = &datasource.SourceRef{
				Type:  c.sourceName,
				RefID: pr.Link,
			}
		}

		items = append(items, datasource.NormalizedItem{
			ID:        fmt.Sprintf("%s:%s:%d", c.sourceName, repo, pr.Number),
			Source:    c.sourceName,
			SourceRef: sourceRef,
			UpdatedAt: updatedAt,
			Content:   content,
			Metadata:  meta,
		})
	}
	return items
}

// fetchIssueItems retrieves all open issues for the repo (paginated, max 100
// pages) and converts each one to a NormalizedItem. PR-backed issues (where
// PullRequest.Number > 0) are skipped since they are already captured by
// fetchPRItems. Errors are logged and an empty slice is returned.
func (c *SCMConnector) fetchIssueItems(ctx context.Context, repo string) []datasource.NormalizedItem {
	var issues []*go_scm.Issue
	opts := go_scm.IssueListOptions{Open: true, Size: 100}
	for page := 1; page <= 100; page++ {
		opts.Page = page
		pageIssues, err := c.svc.ListIssues(ctx, repo, opts)
		if err != nil {
			logger.GetLogger(ctx).Warn("SCM datasource: list issues failed, skipping repo",
				"source", c.sourceName, "repo", repo, "error", err)
			return nil
		}
		if len(pageIssues) == 0 {
			break
		}
		issues = append(issues, pageIssues...)
		if len(pageIssues) < opts.Size {
			break
		}
	}

	items := make([]datasource.NormalizedItem, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		// Skip issues that are really PRs (providers like GitHub surface PRs
		// through the Issues API as well).
		if issue.PullRequest.Number > 0 {
			continue
		}
		content := issue.Title
		if issue.Body != "" {
			content = issue.Title + "\n\n" + issue.Body
		}
		updatedAt := issue.Updated
		if updatedAt.IsZero() {
			updatedAt = issue.Created
		}
		meta := map[string]string{"type": "issue", "title": issue.Title, "repo": repo}
		if issue.Author.Login != "" {
			meta["author"] = issue.Author.Login
		}
		if len(issue.Labels) > 0 {
			meta["labels"] = strings.Join(issue.Labels, ",")
		}

		var sourceRef *datasource.SourceRef
		if issue.Link != "" {
			sourceRef = &datasource.SourceRef{
				Type:  c.sourceName,
				RefID: issue.Link,
			}
		}

		items = append(items, datasource.NormalizedItem{
			ID:        fmt.Sprintf("%s:issue:%s:%d", c.sourceName, repo, issue.Number),
			Source:    c.sourceName,
			SourceRef: sourceRef,
			UpdatedAt: updatedAt,
			Content:   content,
			Metadata:  meta,
		})
	}
	return items
}

// fetchRecentAuthors inspects the last recentCommitCount commits on the repo's
// default head branch and returns a single NormalizedItem that lists the unique
// authors found. If no commits are available the boolean return is false.
func (c *SCMConnector) fetchRecentAuthors(ctx context.Context, repo string) (datasource.NormalizedItem, bool) {
	commits, err := c.svc.ListCommits(ctx, repo, go_scm.CommitListOptions{
		Size: c.recentCommitCount,
		Page: 1,
	})
	if err != nil {
		logger.GetLogger(ctx).Warn("SCM datasource: list commits failed, skipping recent authors",
			"source", c.sourceName, "repo", repo, "error", err)
		return datasource.NormalizedItem{}, false
	}
	if len(commits) == 0 {
		return datasource.NormalizedItem{}, false
	}

	seen := make(map[string]struct{}, len(commits))
	authors := make([]string, 0, len(commits))
	for _, commit := range commits {
		if commit == nil {
			continue
		}
		name := commit.Author.Login
		if name == "" {
			name = commit.Author.Name
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		authors = append(authors, name)
	}

	if len(authors) == 0 {
		return datasource.NormalizedItem{}, false
	}

	var sourceRef *datasource.SourceRef
	// Create a generic ref back to the repo commits page if possible, otherwise just use the repo string.
	// Since we don't have a direct link for "recent authors" overall list, we just omit SourceRef so we don't present a broken link, or if the repo base URL is known we could construct one, but we don't have it here.

	return datasource.NormalizedItem{
		ID:        fmt.Sprintf("%s:authors:%s", c.sourceName, repo),
		Source:    c.sourceName,
		SourceRef: sourceRef,
		Content:   fmt.Sprintf("Recent authors for %s: %s", repo, strings.Join(authors, ", ")),
		Metadata: map[string]string{
			"type":    "authors",
			"repo":    repo,
			"authors": strings.Join(authors, ","),
		},
	}, true
}

// Ensure SCMConnector implements datasource.DataSource at compile time.
var _ datasource.DataSource = (*SCMConnector)(nil)
