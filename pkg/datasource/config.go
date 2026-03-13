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

	// Calendar scopes vectorization to the given calendar IDs. Omit or set
	// Enabled false to skip.
	Calendar *CalendarSourceConfig `yaml:"calendar,omitempty" toml:"calendar,omitempty"`

	// Jira scopes vectorization to the given project keys. Omit or set
	// Enabled false to skip. Requires a Jira MCP server in [mcp] config.
	Jira *JiraSourceConfig `yaml:"jira,omitempty" toml:"jira,omitempty"`

	// Confluence scopes vectorization to the given space keys. Omit or set
	// Enabled false to skip. Requires a Confluence MCP server in [mcp] config.
	Confluence *ConfluenceSourceConfig `yaml:"confluence,omitempty" toml:"confluence,omitempty"`

	// ServiceNow scopes vectorization to the given table names. Omit or set
	// Enabled false to skip. Requires a ServiceNow MCP server in [mcp] config.
	ServiceNow *ServiceNowSourceConfig `yaml:"servicenow,omitempty" toml:"servicenow,omitempty"`

	// ExternalSources holds additional SourceConfig entries registered at
	// runtime (e.g. by SCM config). These are keyed by source name and
	// participate in ScopeFromConfig/EnabledSourceNames automatically.
	ExternalSources map[string]SourceConfig `yaml:"-" toml:"-"`
}

// ── Source Config Types ──────────────────────────────────────────────────
// Each type implements datasource.SourceConfig so ScopeFromConfig and
// EnabledSourceNames work generically.

// GDriveSourceConfig enables and scopes the Google Drive data source.
type GDriveSourceConfig struct {
	Enabled   bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	FolderIDs []string `yaml:"folder_ids,omitempty" toml:"folder_ids,omitempty"`
}

func (c *GDriveSourceConfig) IsEnabled() bool       { return c != nil && c.Enabled && len(c.FolderIDs) > 0 }
func (c *GDriveSourceConfig) ScopeValues() []string { return c.FolderIDs }

// GmailSourceConfig enables and scopes the Gmail data source.
type GmailSourceConfig struct {
	Enabled  bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	LabelIDs []string `yaml:"label_ids,omitempty" toml:"label_ids,omitempty"`
}

func (c *GmailSourceConfig) IsEnabled() bool       { return c != nil && c.Enabled && len(c.LabelIDs) > 0 }
func (c *GmailSourceConfig) ScopeValues() []string { return c.LabelIDs }

// SlackSourceConfig enables and scopes the Slack data source.
type SlackSourceConfig struct {
	Enabled    bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	ChannelIDs []string `yaml:"channel_ids,omitempty" toml:"channel_ids,omitempty"`
}

func (c *SlackSourceConfig) IsEnabled() bool       { return c != nil && c.Enabled && len(c.ChannelIDs) > 0 }
func (c *SlackSourceConfig) ScopeValues() []string { return c.ChannelIDs }

// LinearSourceConfig enables and scopes the Linear data source.
type LinearSourceConfig struct {
	Enabled bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	TeamIDs []string `yaml:"team_ids,omitempty" toml:"team_ids,omitempty"`
}

func (c *LinearSourceConfig) IsEnabled() bool       { return c != nil && c.Enabled && len(c.TeamIDs) > 0 }
func (c *LinearSourceConfig) ScopeValues() []string { return c.TeamIDs }

// CalendarSourceConfig enables and scopes the Calendar data source.
type CalendarSourceConfig struct {
	Enabled     bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	CalendarIDs []string `yaml:"calendar_ids,omitempty" toml:"calendar_ids,omitempty"`
}

func (c *CalendarSourceConfig) IsEnabled() bool {
	return c != nil && c.Enabled && len(c.CalendarIDs) > 0
}
func (c *CalendarSourceConfig) ScopeValues() []string { return c.CalendarIDs }

// JiraSourceConfig enables and scopes the Jira data source.
// Requires a corresponding MCP server named "jira" (or as specified by MCPServer).
type JiraSourceConfig struct {
	Enabled     bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	ProjectKeys []string `yaml:"project_keys,omitempty" toml:"project_keys,omitempty"`
	MCPServer   string   `yaml:"mcp_server,omitempty" toml:"mcp_server,omitempty"`
}

func (c *JiraSourceConfig) IsEnabled() bool       { return c != nil && c.Enabled && len(c.ProjectKeys) > 0 }
func (c *JiraSourceConfig) ScopeValues() []string { return c.ProjectKeys }

// ConfluenceSourceConfig enables and scopes the Confluence data source.
// Requires a corresponding MCP server named "confluence" (or as specified by MCPServer).
type ConfluenceSourceConfig struct {
	Enabled   bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	SpaceKeys []string `yaml:"space_keys,omitempty" toml:"space_keys,omitempty"`
	MCPServer string   `yaml:"mcp_server,omitempty" toml:"mcp_server,omitempty"`
}

func (c *ConfluenceSourceConfig) IsEnabled() bool {
	return c != nil && c.Enabled && len(c.SpaceKeys) > 0
}
func (c *ConfluenceSourceConfig) ScopeValues() []string { return c.SpaceKeys }

// ServiceNowSourceConfig enables and scopes the ServiceNow data source.
// Requires a corresponding MCP server named "servicenow" (or as specified by MCPServer).
type ServiceNowSourceConfig struct {
	Enabled    bool     `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	TableNames []string `yaml:"table_names,omitempty" toml:"table_names,omitempty"`
	MCPServer  string   `yaml:"mcp_server,omitempty" toml:"mcp_server,omitempty"`
}

func (c *ServiceNowSourceConfig) IsEnabled() bool {
	return c != nil && c.Enabled && len(c.TableNames) > 0
}
func (c *ServiceNowSourceConfig) ScopeValues() []string { return c.TableNames }

// sourceConfigs returns a map of source name → SourceConfig for all
// configured sources (built-in + external). This is the single registry that
// ScopeFromConfig and EnabledSourceNames iterate.
func (c *Config) sourceConfigs() map[string]SourceConfig {
	m := map[string]SourceConfig{
		"gdrive":     c.GDrive,
		"gmail":      c.Gmail,
		"slack":      c.Slack,
		"linear":     c.Linear,
		"calendar":   c.Calendar,
		"jira":       c.Jira,
		"confluence": c.Confluence,
		"servicenow": c.ServiceNow,
	}
	for name, sc := range c.ExternalSources {
		m[name] = sc
	}
	return m
}

// ScopeFromConfig builds a Scope from the current Config for the given source name.
// It is used by the sync pipeline to pass the right scope to each connector's ListItems.
// Returns a zero Scope when the source is disabled or has no scope configured.
func (c *Config) ScopeFromConfig(sourceName string) Scope {
	if c == nil {
		return Scope{}
	}
	sc, ok := c.sourceConfigs()[sourceName]
	if !ok || sc == nil || !sc.IsEnabled() {
		return Scope{}
	}
	return NewScope(sourceName, sc.ScopeValues())
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
	for name, sc := range c.sourceConfigs() {
		if sc != nil && sc.IsEnabled() {
			out = append(out, name)
		}
	}
	return out
}
