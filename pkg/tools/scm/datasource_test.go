package scm_test

import (
	"context"
	"time"

	go_scm "github.com/drone/go-scm/scm"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/tools/scm"
	"github.com/stackgenhq/genie/pkg/tools/scm/scmfakes"
)

var _ = Describe("SCMConnector", func() {
	var fake *scmfakes.FakeService

	BeforeEach(func() {
		fake = new(scmfakes.FakeService)
	})

	Describe("Name", func() {
		It("returns github", func() {
			conn := scm.NewGitHubConnector(fake)
			Expect(conn.Name()).To(Equal("github"))
		})
	})

	Describe("ListItems", func() {
		It("returns nil when scope has no repos for source", func(ctx context.Context) {
			conn := scm.NewSCMConnector(fake, "github")
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
			Expect(fake.ListPullRequestsCallCount()).To(Equal(0))
		})

		It("returns normalized items for pull requests in scope repos", func(ctx context.Context) {
			now := time.Now()
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{
				{
					Number:  42,
					Title:   "Add feature",
					Body:    "Description of the PR",
					Closed:  false,
					Created: now,
					Updated: now,
					Author:  go_scm.User{Login: "dev"},
				},
			}, nil)
			conn := scm.NewGitHubConnector(fake)
			scope := datasource.Scope{GitHubRepos: []string{"owner/repo"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("github:owner/repo:42"))
			Expect(items[0].Source).To(Equal("github"))
			Expect(items[0].Content).To(ContainSubstring("Add feature"))
			Expect(items[0].Content).To(ContainSubstring("Description of the PR"))
			Expect(items[0].Metadata["title"]).To(Equal("Add feature"))
			Expect(items[0].Metadata["state"]).To(Equal("open"))
			Expect(items[0].Metadata["author"]).To(Equal("dev"))

			Expect(fake.ListPullRequestsCallCount()).To(Equal(1))
			_, repo, _ := fake.ListPullRequestsArgsForCall(0)
			Expect(repo).To(Equal("owner/repo"))
		})

		It("returns repo metadata item when FindRepo succeeds", func(ctx context.Context) {
			fake.FindRepoReturns(&go_scm.Repository{
				Namespace:   "owner",
				Name:        "repo",
				Link:        "https://github.com/owner/repo",
				Description: "A test repo",
				Language:    go_scm.RepoLanguages{"Go": 80, "Makefile": 20},
				Updated:     time.Now(),
			}, nil)
			fake.ListPullRequestsReturns(nil, nil)
			conn := scm.NewGitHubConnector(fake)
			scope := datasource.Scope{GitHubRepos: []string{"owner/repo"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("github:repo:owner/repo"))
			Expect(items[0].Metadata["type"]).To(Equal("repo"))
			Expect(items[0].Metadata["url"]).To(Equal("https://github.com/owner/repo"))
			Expect(items[0].Content).To(ContainSubstring("A test repo"))
			Expect(items[0].Content).To(ContainSubstring("https://github.com/owner/repo"))
			Expect(items[0].Metadata["language"]).To(ContainSubstring("Go"))
		})

		It("uses sourceName in item IDs and Source for any provider", func(ctx context.Context) {
			fake.ListPullRequestsReturns([]*go_scm.PullRequest{
				{Number: 1, Title: "MR title", Body: "Body", Closed: false, Created: time.Now(), Updated: time.Now(), Author: go_scm.User{Login: "dev"}},
			}, nil)
			conn := scm.NewSCMConnector(fake, "gitlab")
			scope := datasource.Scope{GitLabRepos: []string{"group/project"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("gitlab:group/project:1"))
			Expect(items[0].Source).To(Equal("gitlab"))
			Expect(items[0].Metadata["title"]).To(Equal("MR title"))
		})
	})
})
