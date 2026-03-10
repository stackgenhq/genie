// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package datasource_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
)

var _ = Describe("Config", func() {
	Describe("ScopeFromConfig", func() {
		It("returns zero scope when config is nil", func() {
			var c *datasource.Config
			scope := c.ScopeFromConfig("slack")
			Expect(scope).To(Equal(datasource.Scope{}))
		})

		It("returns zero scope when source is disabled", func() {
			c := &datasource.Config{
				Slack: &datasource.SlackSourceConfig{Enabled: false, ChannelIDs: []string{"C1"}},
			}
			scope := c.ScopeFromConfig("slack")
			Expect(scope).To(Equal(datasource.Scope{}))
		})

		It("returns zero scope when source has empty scope", func() {
			c := &datasource.Config{
				Slack: &datasource.SlackSourceConfig{Enabled: true, ChannelIDs: nil},
			}
			scope := c.ScopeFromConfig("slack")
			Expect(scope).To(Equal(datasource.Scope{}))
		})

		It("returns gdrive scope when gdrive is enabled with folder IDs", func() {
			c := &datasource.Config{
				GDrive: &datasource.GDriveSourceConfig{Enabled: true, FolderIDs: []string{"folder1", "folder2"}},
			}
			scope := c.ScopeFromConfig("gdrive")
			Expect(scope.GDriveFolderIDs).To(Equal([]string{"folder1", "folder2"}))
		})

		It("returns gmail scope when gmail is enabled with label IDs", func() {
			c := &datasource.Config{
				Gmail: &datasource.GmailSourceConfig{Enabled: true, LabelIDs: []string{"INBOX", "Label_1"}},
			}
			scope := c.ScopeFromConfig("gmail")
			Expect(scope.GmailLabelIDs).To(Equal([]string{"INBOX", "Label_1"}))
		})

		It("returns slack scope when slack is enabled with channel IDs", func() {
			c := &datasource.Config{
				Slack: &datasource.SlackSourceConfig{Enabled: true, ChannelIDs: []string{"C1", "C2"}},
			}
			scope := c.ScopeFromConfig("slack")
			Expect(scope.SlackChannelIDs).To(Equal([]string{"C1", "C2"}))
		})

		It("returns linear scope when linear is enabled with team IDs", func() {
			c := &datasource.Config{
				Linear: &datasource.LinearSourceConfig{Enabled: true, TeamIDs: []string{"team1"}},
			}
			scope := c.ScopeFromConfig("linear")
			Expect(scope.LinearTeamIDs).To(Equal([]string{"team1"}))
		})

		It("returns github scope when github is enabled with repos", func() {
			c := &datasource.Config{
				GitHub: &datasource.GitHubSourceConfig{Enabled: true, Repos: []string{"owner/repo"}},
			}
			scope := c.ScopeFromConfig("github")
			Expect(scope.GitHubRepos).To(Equal([]string{"owner/repo"}))
		})
		It("returns gitlab scope when gitlab is enabled with repos", func() {
			c := &datasource.Config{
				GitLab: &datasource.GitLabSourceConfig{Enabled: true, Repos: []string{"group/project"}},
			}
			scope := c.ScopeFromConfig("gitlab")
			Expect(scope.GitLabRepos).To(Equal([]string{"group/project"}))
		})
		It("returns calendar scope when calendar is enabled with calendar IDs", func() {
			c := &datasource.Config{
				Calendar: &datasource.CalendarSourceConfig{Enabled: true, CalendarIDs: []string{"primary"}},
			}
			scope := c.ScopeFromConfig("calendar")
			Expect(scope.CalendarIDs).To(Equal([]string{"primary"}))
		})

		It("returns zero scope for unknown source name", func() {
			c := &datasource.Config{Slack: &datasource.SlackSourceConfig{Enabled: true, ChannelIDs: []string{"C1"}}}
			scope := c.ScopeFromConfig("unknown")
			Expect(scope).To(Equal(datasource.Scope{}))
		})
	})

	Describe("EnabledSourceNames", func() {
		It("returns nil when config is nil", func() {
			var c *datasource.Config
			names := c.EnabledSourceNames()
			Expect(names).To(BeNil())
		})

		It("returns nil when data sources layer is disabled", func() {
			c := &datasource.Config{
				Enabled: false,
				Slack:   &datasource.SlackSourceConfig{Enabled: true, ChannelIDs: []string{"C1"}},
			}
			names := c.EnabledSourceNames()
			Expect(names).To(BeNil())
		})

		It("returns only sources that are enabled and have non-empty scope", func() {
			c := &datasource.Config{
				Enabled: true,
				GDrive:  &datasource.GDriveSourceConfig{Enabled: true, FolderIDs: []string{"f1"}},
				Gmail:   &datasource.GmailSourceConfig{Enabled: true, LabelIDs: nil},
				Slack:   &datasource.SlackSourceConfig{Enabled: true, ChannelIDs: []string{"C1"}},
			}
			names := c.EnabledSourceNames()
			Expect(names).To(ConsistOf("gdrive", "slack"))
		})

		It("returns all enabled sources when all have scope", func() {
			c := &datasource.Config{
				Enabled:  true,
				GDrive:   &datasource.GDriveSourceConfig{Enabled: true, FolderIDs: []string{"f1"}},
				Gmail:    &datasource.GmailSourceConfig{Enabled: true, LabelIDs: []string{"INBOX"}},
				Slack:    &datasource.SlackSourceConfig{Enabled: true, ChannelIDs: []string{"C1"}},
				Linear:   &datasource.LinearSourceConfig{Enabled: true, TeamIDs: []string{"t1"}},
				GitHub:   &datasource.GitHubSourceConfig{Enabled: true, Repos: []string{"o/r"}},
				Calendar: &datasource.CalendarSourceConfig{Enabled: true, CalendarIDs: []string{"primary"}},
			}
			names := c.EnabledSourceNames()
			Expect(names).To(ConsistOf("gdrive", "gmail", "slack", "linear", "github", "calendar"))
		})
	})

	Describe("SearchKeywordsTrimmed", func() {
		It("returns nil when config is nil or SearchKeywords empty", func() {
			var c *datasource.Config
			Expect(c.SearchKeywordsTrimmed()).To(BeNil())
			Expect((&datasource.Config{}).SearchKeywordsTrimmed()).To(BeNil())
		})
		It("trims and deduplicates and caps at MaxSearchKeywords", func() {
			c := &datasource.Config{SearchKeywords: []string{" Acme ", "acme", " Q4 ", "", "x"}}
			Expect(c.SearchKeywordsTrimmed()).To(Equal([]string{"Acme", "Q4", "x"}))
			c.SearchKeywords = make([]string, 12)
			for i := range c.SearchKeywords {
				c.SearchKeywords[i] = string(rune('a' + i))
			}
			Expect(c.SearchKeywordsTrimmed()).To(HaveLen(datasource.MaxSearchKeywords))
		})
	})

	Describe("ItemMatchesKeywords", func() {
		It("returns true when keywords is nil or empty", func() {
			item := &datasource.NormalizedItem{Content: "anything"}
			Expect(datasource.ItemMatchesKeywords(item, nil)).To(BeTrue())
			Expect(datasource.ItemMatchesKeywords(item, []string{})).To(BeTrue())
		})
		It("returns true when content contains a keyword (case-insensitive)", func() {
			item := &datasource.NormalizedItem{Content: "Meeting about ACME project"}
			Expect(datasource.ItemMatchesKeywords(item, []string{"acme"})).To(BeTrue())
			Expect(datasource.ItemMatchesKeywords(item, []string{"Project"})).To(BeTrue())
		})
		It("returns true when metadata value contains a keyword", func() {
			item := &datasource.NormalizedItem{Content: "Body", Metadata: map[string]string{"subject": "Q4 roadmap"}}
			Expect(datasource.ItemMatchesKeywords(item, []string{"Q4"})).To(BeTrue())
		})
		It("returns false when no keyword matches", func() {
			item := &datasource.NormalizedItem{Content: "Random email", Metadata: map[string]string{"from": "a@b.com"}}
			Expect(datasource.ItemMatchesKeywords(item, []string{"Acme", "onboarding"})).To(BeFalse())
		})
	})
})
