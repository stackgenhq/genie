// Package scm provides a single DataSource connector backed by go-scm for all
// SCM providers (GitHub, GitLab, Bitbucket). It lists pull requests and repo
// metadata (name, URL, description, language) for each repo in scope and returns
// them as NormalizedItems for vectorization. One adapter serves all providers.
package scm

import (
	"context"
	"fmt"
	"strings"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/logger"
)

// SCMConnector implements datasource.DataSource for any go-scm provider (GitHub,
// GitLab, Bitbucket). It is parameterized by sourceName so one implementation
// serves all; scope.ReposForSCM(sourceName) supplies the repo list per provider.
type SCMConnector struct {
	svc        Service
	sourceName string
}

// NewSCMConnector returns a DataSource that uses the go-scm Service to list
// repo metadata and pull requests. sourceName is the datasource identifier
// (e.g. "github", "gitlab"); scope.ReposForSCM(sourceName) defines which repos to include.
func NewSCMConnector(svc Service, sourceName string) *SCMConnector {
	return &SCMConnector{svc: svc, sourceName: sourceName}
}

// NewGitHubConnector returns a DataSource for GitHub using the shared go-scm adapter.
func NewGitHubConnector(svc Service) *SCMConnector {
	return NewSCMConnector(svc, "github")
}

// Name returns the source identifier (e.g. "github", "gitlab").
func (c *SCMConnector) Name() string {
	return c.sourceName
}

// ListItems lists repo metadata and pull requests for each repo in scope for this
// SCM source. Uses scope.ReposForSCM(c.sourceName) so one connector works for
// all go-scm providers. Item IDs use sourceName (e.g. "github:repo:owner/repo").
func (c *SCMConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	repos := scope.ReposForSCM(c.sourceName)
	if len(repos) == 0 {
		return nil, nil
	}
	var out []datasource.NormalizedItem
	name := c.sourceName

	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		// Repo metadata (name, URL, description, language)
		if r, err := c.svc.FindRepo(ctx, repo); err == nil && r != nil {
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
			out = append(out, datasource.NormalizedItem{
				ID:        fmt.Sprintf("%s:repo:%s", name, repo),
				Source:    name,
				UpdatedAt: updatedAt,
				Content:   content,
				Metadata:  meta,
			})
		}
		// Pull requests (paginated to avoid silent truncation; max 100 pages to prevent unbounded requests)
		var prs []*go_scm.PullRequest
		opts := go_scm.PullRequestListOptions{Open: true, Size: 100}
		for page := 1; page <= 100; page++ {
			opts.Page = page
			pagePRs, err := c.svc.ListPullRequests(ctx, repo, opts)
			if err != nil {
				// Skip PRs for this repo on error to match FindRepo behavior; log so sync can report partial failure.
				logger.GetLogger(ctx).Warn("SCM datasource: list pull requests failed, skipping repo", "source", name, "repo", repo, "error", err)
				prs = nil
				break
			}
			if len(pagePRs) == 0 {
				break
			}
			prs = append(prs, pagePRs...)
			if len(pagePRs) < opts.Size {
				break
			}
		}
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
			out = append(out, datasource.NormalizedItem{
				ID:        fmt.Sprintf("%s:%s:%d", name, repo, pr.Number),
				Source:    name,
				UpdatedAt: updatedAt,
				Content:   content,
				Metadata:  meta,
			})
		}
	}
	return out, nil
}

// Ensure SCMConnector implements datasource.DataSource at compile time.
var _ datasource.DataSource = (*SCMConnector)(nil)
