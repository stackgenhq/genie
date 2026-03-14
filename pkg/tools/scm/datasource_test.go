// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	go_scm "github.com/drone/go-scm/scm"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/tools/scm"
	"github.com/stackgenhq/genie/pkg/tools/scm/scmfakes"
)

var _ = Describe("SCMConnector", func() {
	var (
		fake      *scmfakes.FakeService
		connector *scm.SCMConnector
		ctx       context.Context
	)

	BeforeEach(func() {
		fake = new(scmfakes.FakeService)
		connector = scm.NewSCMConnector(fake)
		ctx = context.Background()
	})

	// Helper to build a scope that references the connector under test.
	// SCMConnector.Name() returns "" when created via NewSCMConnector (sourceName
	// is only set by the registry path), so we build the scope with the empty key.
	makeScope := func(repos ...string) datasource.Scope {
		return datasource.NewScope("", repos)
	}

	Describe("ListItems", func() {
		Context("when scope is empty", func() {
			It("returns nil without calling the service", func() {
				items, err := connector.ListItems(ctx, datasource.Scope{})
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(BeNil())
				Expect(fake.FindRepoCallCount()).To(Equal(0))
			})
		})

		Context("when the repo is blank after trimming", func() {
			It("skips blank entries and returns nil", func() {
				items, err := connector.ListItems(ctx, makeScope("   ", ""))
				Expect(err).NotTo(HaveOccurred())
				Expect(items).To(BeNil())
				Expect(fake.FindRepoCallCount()).To(Equal(0))
			})
		})

		Context("with a valid repo", func() {
			BeforeEach(func() {
				// Stub FindRepo
				now := time.Now()
				fake.FindRepoReturns(&go_scm.Repository{
					Description: "my repo",
					Link:        "https://github.com/owner/repo",
					Language:    go_scm.RepoLanguages{"Go": 100, "Bash": 10},
					Updated:     now,
				}, nil)
				// Stub ListPullRequests — single page, fewer than 100 items → stops
				fake.ListPullRequestsReturns([]*go_scm.PullRequest{
					{Number: 1, Title: "Fix bug", Body: "desc", Closed: false,
						Author: go_scm.User{Login: "alice"}, Updated: now},
				}, nil)
				// Stub ListIssues — single page
				fake.ListIssuesReturns([]*go_scm.Issue{
					{Number: 10, Title: "Open issue", Body: "body", Author: go_scm.User{Login: "bob"},
						Labels: []string{"bug"}, Updated: now},
				}, nil)
				// Stub ListCommits — two authors, one duplicate
				fake.ListCommitsReturns([]*go_scm.Commit{
					{Author: go_scm.Signature{Login: "alice"}},
					{Author: go_scm.Signature{Login: "bob"}},
					{Author: go_scm.Signature{Login: "alice"}}, // duplicate, should be deduplicated
				}, nil)
			})

			It("returns items from all four fetchers", func() {
				items, err := connector.ListItems(ctx, makeScope("owner/repo"))
				Expect(err).NotTo(HaveOccurred())

				types := make(map[string]int)
				for _, item := range items {
					types[item.Metadata["type"]]++
				}

				Expect(types["repo"]).To(Equal(1))
				Expect(types["pr"]).To(Equal(1))
				Expect(types["issue"]).To(Equal(1))
				Expect(types["authors"]).To(Equal(1))
			})
		})
	})

	Describe("fetchRepoItem (via ListItems)", func() {
		It("builds correct metadata for a repo with description, link and language", func() {
			now := time.Now()
			fake.FindRepoReturns(&go_scm.Repository{
				Description: "awesome project",
				Link:        "https://github.com/owner/repo",
				Language:    go_scm.RepoLanguages{"Go": 90, "Bash": 10},
				Updated:     now,
			}, nil)
			fake.ListPullRequestsReturns(nil, nil)
			fake.ListIssuesReturns(nil, nil)
			fake.ListCommitsReturns(nil, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			repoItems := filterByType(items, "repo")
			Expect(repoItems).To(HaveLen(1))

			item := repoItems[0]
			Expect(item.Metadata["name"]).To(Equal("owner/repo"))
			Expect(item.Metadata["url"]).To(Equal("https://github.com/owner/repo"))
			Expect(item.SourceRef).NotTo(BeNil())
			Expect(item.SourceRef.Type).To(Equal("")) // sourceName is empty in tests
			Expect(item.SourceRef.RefID).To(Equal("https://github.com/owner/repo"))
			Expect(item.Content).To(ContainSubstring("awesome project"))
			Expect(item.Content).To(ContainSubstring("https://github.com/owner/repo"))
			Expect(item.Metadata["language"]).NotTo(BeEmpty())
			Expect(item.UpdatedAt).To(Equal(now))
		})

		It("falls back to Created when Updated is zero", func() {
			created := time.Now().Add(-24 * time.Hour)
			fake.FindRepoReturns(&go_scm.Repository{
				Created: created,
			}, nil)
			fake.ListPullRequestsReturns(nil, nil)
			fake.ListIssuesReturns(nil, nil)
			fake.ListCommitsReturns(nil, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			repoItems := filterByType(items, "repo")
			Expect(repoItems).To(HaveLen(1))
			Expect(repoItems[0].UpdatedAt).To(Equal(created))
		})

		It("produces no repo item when FindRepo fails", func() {
			fake.FindRepoReturns(nil, fmt.Errorf("not found"))
			fake.ListPullRequestsReturns(nil, nil)
			fake.ListIssuesReturns(nil, nil)
			fake.ListCommitsReturns(nil, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())
			Expect(filterByType(items, "repo")).To(BeEmpty())
		})
	})

	Describe("fetchPRItems (via ListItems)", func() {
		BeforeEach(func() {
			fake.FindRepoReturns(nil, fmt.Errorf("skip"))
			fake.ListIssuesReturns(nil, nil)
			fake.ListCommitsReturns(nil, nil)
		})

		It("converts open PRs to NormalizedItems with correct metadata", func() {
			now := time.Now()
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{
				{Number: 7, Title: "Feature", Body: "details", Closed: false, Link: "https://pr.link",
					Author: go_scm.User{Login: "dev"}, Updated: now},
			}, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			prItems := filterByType(items, "pr")
			Expect(prItems).To(HaveLen(1))

			item := prItems[0]
			Expect(item.Metadata["state"]).To(Equal("open"))
			Expect(item.Metadata["author"]).To(Equal("dev"))
			Expect(item.Metadata["title"]).To(Equal("Feature"))
			Expect(item.SourceRef).NotTo(BeNil())
			Expect(item.SourceRef.RefID).To(Equal("https://pr.link"))
			Expect(item.Content).To(ContainSubstring("details"))
			Expect(item.UpdatedAt).To(Equal(now))
		})

		It("marks closed PRs correctly", func() {
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{
				{Number: 2, Title: "Old PR", Closed: true, Updated: time.Now()},
			}, nil)

			items, _ := connector.ListItems(ctx, makeScope("owner/repo"))
			prItems := filterByType(items, "pr")
			Expect(prItems).To(HaveLen(1))
			Expect(prItems[0].Metadata["state"]).To(Equal("closed"))
		})

		It("returns no PR items when ListPullRequests fails", func() {
			fake.ListPullRequestsReturns(nil, fmt.Errorf("forbidden"))

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())
			Expect(filterByType(items, "pr")).To(BeEmpty())
		})

		It("skips nil PR entries", func() {
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{nil, {Number: 3, Title: "Real PR", Updated: time.Now()}}, nil)

			items, _ := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(filterByType(items, "pr")).To(HaveLen(1))
		})
	})

	Describe("fetchIssueItems (via ListItems)", func() {
		BeforeEach(func() {
			fake.FindRepoReturns(nil, fmt.Errorf("skip"))
			fake.ListPullRequestsReturns(nil, nil)
			fake.ListCommitsReturns(nil, nil)
		})

		It("converts open issues to NormalizedItems with correct metadata", func() {
			now := time.Now()
			fake.ListIssuesReturns([]*go_scm.Issue{
				{Number: 42, Title: "Bug report", Body: "details", Link: "https://issue.link",
					Author: go_scm.User{Login: "reporter"}, Labels: []string{"bug", "urgent"}, Updated: now},
			}, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			issueItems := filterByType(items, "issue")
			Expect(issueItems).To(HaveLen(1))

			item := issueItems[0]
			Expect(item.Metadata["title"]).To(Equal("Bug report"))
			Expect(item.Metadata["author"]).To(Equal("reporter"))
			Expect(item.Metadata["labels"]).To(ContainSubstring("bug"))
			Expect(item.SourceRef).NotTo(BeNil())
			Expect(item.SourceRef.RefID).To(Equal("https://issue.link"))
			Expect(item.Content).To(ContainSubstring("details"))
			Expect(item.UpdatedAt).To(Equal(now))
			Expect(item.ID).To(ContainSubstring(":issue:"))
		})

		It("skips issues that are backed by pull requests", func() {
			fake.ListIssuesReturns([]*go_scm.Issue{
				{Number: 1, Title: "PR-backed", PullRequest: go_scm.PullRequest{Number: 1}},
				{Number: 2, Title: "Real issue", Updated: time.Now()},
			}, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			issueItems := filterByType(items, "issue")
			Expect(issueItems).To(HaveLen(1))
			Expect(issueItems[0].Metadata["title"]).To(Equal("Real issue"))
		})

		It("returns no issue items when ListIssues fails", func() {
			fake.ListIssuesReturns(nil, fmt.Errorf("rate limited"))

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())
			Expect(filterByType(items, "issue")).To(BeEmpty())
		})

		It("skips nil issue entries", func() {
			fake.ListIssuesReturns([]*go_scm.Issue{nil, {Number: 5, Title: "Valid", Updated: time.Now()}}, nil)

			items, _ := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(filterByType(items, "issue")).To(HaveLen(1))
		})
	})

	Describe("fetchRecentAuthors (via ListItems)", func() {
		BeforeEach(func() {
			fake.FindRepoReturns(nil, fmt.Errorf("skip"))
			fake.ListPullRequestsReturns(nil, nil)
			fake.ListIssuesReturns(nil, nil)
		})

		It("produces an authors item with deduplicated author names", func() {
			fake.ListCommitsReturns([]*go_scm.Commit{
				{Author: go_scm.Signature{Login: "alice"}},
				{Author: go_scm.Signature{Login: "bob"}},
				{Author: go_scm.Signature{Login: "alice"}},           // duplicate
				{Author: go_scm.Signature{Name: "Carol", Login: ""}}, // no login, use Name
			}, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			authorItems := filterByType(items, "authors")
			Expect(authorItems).To(HaveLen(1))

			item := authorItems[0]
			Expect(item.Metadata["authors"]).To(ContainSubstring("alice"))
			Expect(item.Metadata["authors"]).To(ContainSubstring("bob"))
			Expect(item.Metadata["authors"]).To(ContainSubstring("Carol"))
			// alice should appear only once
			Expect(countOccurrences(item.Metadata["authors"], "alice")).To(Equal(1))
		})

		It("returns no authors item when ListCommits fails", func() {
			fake.ListCommitsReturns(nil, fmt.Errorf("server error"))

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())
			Expect(filterByType(items, "authors")).To(BeEmpty())
		})

		It("returns no authors item when commits list is empty", func() {
			fake.ListCommitsReturns(nil, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())
			Expect(filterByType(items, "authors")).To(BeEmpty())
		})

		It("skips commits with no login and no name", func() {
			fake.ListCommitsReturns([]*go_scm.Commit{
				{Author: go_scm.Signature{}}, // empty author
				{Author: go_scm.Signature{Login: "trudy"}},
			}, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo"))
			Expect(err).NotTo(HaveOccurred())

			authorItems := filterByType(items, "authors")
			Expect(authorItems).To(HaveLen(1))
			Expect(authorItems[0].Metadata["authors"]).To(Equal("trudy"))
		})

		It("passes recentCommitCount as the page size", func() {
			customConnector := scm.NewSCMConnectorWithOptions(fake, 10)
			fake.ListCommitsReturns([]*go_scm.Commit{
				{Author: go_scm.Signature{Login: "dev"}},
			}, nil)

			scope := datasource.NewScope("", []string{"owner/repo"})
			_, err := customConnector.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())

			Expect(fake.ListCommitsCallCount()).To(BeNumerically(">=", 1))
			_, _, opts := fake.ListCommitsArgsForCall(0)
			Expect(opts.Size).To(Equal(10))
		})
	})

	Describe("concurrent multi-repo fetching", func() {
		It("processes multiple repos and returns items from all of them", func() {
			now := time.Now()
			// Both repos succeed
			fake.FindRepoReturnsOnCall(0, &go_scm.Repository{Updated: now}, nil)
			fake.FindRepoReturnsOnCall(1, &go_scm.Repository{Updated: now}, nil)
			fake.ListPullRequestsReturns(nil, nil)
			fake.ListIssuesReturns(nil, nil)
			fake.ListCommitsReturns(nil, nil)

			items, err := connector.ListItems(ctx, makeScope("owner/repo1", "owner/repo2"))
			Expect(err).NotTo(HaveOccurred())

			repoItems := filterByType(items, "repo")
			Expect(repoItems).To(HaveLen(2))
		})
	})
})

// filterByType is a test helper that returns only items with the given type metadata value.
func filterByType(items []datasource.NormalizedItem, t string) []datasource.NormalizedItem {
	var out []datasource.NormalizedItem
	for _, item := range items {
		if item.Metadata["type"] == t {
			out = append(out, item)
		}
	}
	return out
}

// countOccurrences counts non-overlapping occurrences of sub in s.
func countOccurrences(s, sub string) int {
	count := 0
	idx := 0
	for {
		pos := indexOf(s[idx:], sub)
		if pos < 0 {
			break
		}
		count++
		idx += pos + len(sub)
	}
	return count
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
