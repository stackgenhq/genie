// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package datasource

import (
	"strings"
	"time"
)

// MaxSearchKeywords is the maximum number of search keywords allowed (setup and config).
const MaxSearchKeywords = 10

// Config holds the unified data sources configuration: a master switch,
// optional sync schedule, and per-source enable/scope. Credentials for each
// system (e.g. [google_drive], email, Slack) remain in their existing config
// sections; this block only controls which sources are vectorized and their
// scope (folders, channels, labels, etc.). When a source is disabled or its
// scope is empty, the sync pipeline skips it.
type Config struct {
	// Enabled turns the data sources layer on or off. When false, no sync
	// runs and no connector is instantiated for vectorization.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`

	// SyncInterval is how often the sync job runs (e.g. "15m", "1h"). When
	// empty or zero, sync is on-demand only (e.g. via a tool or admin API).
	SyncInterval time.Duration `yaml:"sync_interval,omitempty" toml:"sync_interval,omitempty"`

	// SearchKeywords limits which items are indexed: only items whose content or
	// metadata contains at least one of these keywords (case-insensitive) are
	// embedded. Up to MaxSearchKeywords (10). When empty, all items are indexed.
	SearchKeywords []string `yaml:"search_keywords,omitempty" toml:"search_keywords,omitempty"`

	// GDrive scopes vectorization to the given folder IDs. Credentials come
	// from the main [google_drive] (or env). Omit or set Enabled false to skip.
	GDrive *GDriveSourceConfig `yaml:"gdrive,omitempty" toml:"gdrive,omitempty"`

	// Gmail scopes vectorization to the given labels. Credentials come from
	// the email/Gmail config. Omit or set Enabled false to skip.
	Gmail *GmailSourceConfig `yaml:"gmail,omitempty" toml:"gmail,omitempty"`

	// Slack scopes vectorization to the given channel IDs. Omit or set
	// Enabled false to skip.
	Slack *SlackSourceConfig `yaml:"slack,omitempty" toml:"slack,omitempty"`

	// Linear scopes vectorization to the given team IDs. Omit or set
	// Enabled false to skip.
	Linear *LinearSourceConfig `yaml:"linear,omitempty" toml:"linear,omitempty"`

	// GitHub scopes vectorization to the given repos (owner/repo). Omit or
	// set Enabled false to skip.
	GitHub *GitHubSourceConfig `yaml:"github,omitempty" toml:"github,omitempty"`

	// GitLab scopes vectorization to the given repos (project path or owner/repo). Omit or
	// set Enabled false to skip.
	GitLab *GitLabSourceConfig `yaml:"gitlab,omitempty" toml:"gitlab,omitempty"`

	// Calendar scopes vectorization to the given calendar IDs. Omit or set
	// Enabled false to skip.
	Calendar *CalendarSourceConfig `yaml:"calendar,omitempty" toml:"calendar,omitempty"`
}

// GDriveSourceConfig enables and scopes the Google Drive data source.
type GDriveSourceConfig struct {
	Enabled   bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	FolderIDs []string `yaml:"folder_ids,omitempty" toml:"folder_ids,omitempty"`
}

// GmailSourceConfig enables and scopes the Gmail data source.
type GmailSourceConfig struct {
	Enabled  bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	LabelIDs []string `yaml:"label_ids,omitempty" toml:"label_ids,omitempty"`
}

// SlackSourceConfig enables and scopes the Slack data source.
type SlackSourceConfig struct {
	Enabled    bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	ChannelIDs []string `yaml:"channel_ids,omitempty" toml:"channel_ids,omitempty"`
}

// LinearSourceConfig enables and scopes the Linear data source.
type LinearSourceConfig struct {
	Enabled bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	TeamIDs []string `yaml:"team_ids,omitempty" toml:"team_ids,omitempty"`
}

// GitHubSourceConfig enables and scopes the GitHub data source.
type GitHubSourceConfig struct {
	Enabled bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	Repos   []string `yaml:"repos,omitempty" toml:"repos,omitempty"` // "owner/repo"
}

// GitLabSourceConfig enables and scopes the GitLab data source (same go-scm adapter as GitHub).
type GitLabSourceConfig struct {
	Enabled bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	Repos   []string `yaml:"repos,omitempty" toml:"repos,omitempty"` // "owner/repo" or project path
}

// CalendarSourceConfig enables and scopes the Calendar data source.
type CalendarSourceConfig struct {
	Enabled     bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	CalendarIDs []string `yaml:"calendar_ids,omitempty" toml:"calendar_ids,omitempty"`
}

// ScopeFromConfig builds a Scope from the current Config for the given source name.
// It is used by the sync pipeline to pass the right scope to each connector's ListItems.
// Returns a zero Scope when the source is disabled or has no scope configured.
func (c *Config) ScopeFromConfig(sourceName string) Scope {
	if c == nil {
		return Scope{}
	}
	switch sourceName {
	case "gdrive":
		if c.GDrive == nil || !c.GDrive.Enabled || len(c.GDrive.FolderIDs) == 0 {
			return Scope{}
		}
		return Scope{GDriveFolderIDs: c.GDrive.FolderIDs}
	case "gmail":
		if c.Gmail == nil || !c.Gmail.Enabled || len(c.Gmail.LabelIDs) == 0 {
			return Scope{}
		}
		return Scope{GmailLabelIDs: c.Gmail.LabelIDs}
	case "slack":
		if c.Slack == nil || !c.Slack.Enabled || len(c.Slack.ChannelIDs) == 0 {
			return Scope{}
		}
		return Scope{SlackChannelIDs: c.Slack.ChannelIDs}
	case "linear":
		if c.Linear == nil || !c.Linear.Enabled || len(c.Linear.TeamIDs) == 0 {
			return Scope{}
		}
		return Scope{LinearTeamIDs: c.Linear.TeamIDs}
	case "github":
		if c.GitHub == nil || !c.GitHub.Enabled || len(c.GitHub.Repos) == 0 {
			return Scope{}
		}
		return Scope{GitHubRepos: c.GitHub.Repos}
	case "gitlab":
		if c.GitLab == nil || !c.GitLab.Enabled || len(c.GitLab.Repos) == 0 {
			return Scope{}
		}
		return Scope{GitLabRepos: c.GitLab.Repos}
	case "calendar":
		if c.Calendar == nil || !c.Calendar.Enabled || len(c.Calendar.CalendarIDs) == 0 {
			return Scope{}
		}
		return Scope{CalendarIDs: c.Calendar.CalendarIDs}
	default:
		return Scope{}
	}
}

// SearchKeywordsTrimmed returns SearchKeywords limited to MaxSearchKeywords,
// with empty strings and duplicates (case-insensitive) removed.
func (c *Config) SearchKeywordsTrimmed() []string {
	if c == nil || len(c.SearchKeywords) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, k := range c.SearchKeywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		lower := strings.ToLower(k)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, k)
		if len(out) >= MaxSearchKeywords {
			break
		}
	}
	return out
}

// ItemMatchesKeywords returns true if item content or any metadata value
// contains at least one of the keywords (substring, case-insensitive).
// When keywords is nil or empty, returns true (no filter).
func ItemMatchesKeywords(item *NormalizedItem, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	contentLower := strings.ToLower(item.Content)
	for _, v := range item.Metadata {
		contentLower += " " + strings.ToLower(v)
	}
	for _, kw := range keywords {
		if strings.Contains(contentLower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// EnabledSourceNames returns the list of source names that are enabled and
// have non-empty scope in this config. The sync pipeline can iterate this to
// decide which connectors to run.
func (c *Config) EnabledSourceNames() []string {
	if c == nil || !c.Enabled {
		return nil
	}
	var out []string
	if c.GDrive != nil && c.GDrive.Enabled && len(c.GDrive.FolderIDs) > 0 {
		out = append(out, "gdrive")
	}
	if c.Gmail != nil && c.Gmail.Enabled && len(c.Gmail.LabelIDs) > 0 {
		out = append(out, "gmail")
	}
	if c.Slack != nil && c.Slack.Enabled && len(c.Slack.ChannelIDs) > 0 {
		out = append(out, "slack")
	}
	if c.Linear != nil && c.Linear.Enabled && len(c.Linear.TeamIDs) > 0 {
		out = append(out, "linear")
	}
	if c.GitHub != nil && c.GitHub.Enabled && len(c.GitHub.Repos) > 0 {
		out = append(out, "github")
	}
	if c.Calendar != nil && c.Calendar.Enabled && len(c.Calendar.CalendarIDs) > 0 {
		out = append(out, "calendar")
	}
	return out
}
