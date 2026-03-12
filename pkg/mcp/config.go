// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"time"
)

// MCPConfig represents the top-level MCP configuration for multiple server connections.
// This configuration enables Genie to connect to MCP servers via different transports
// (stdio, HTTP, SSE) and manage tool discovery and execution.
type MCPConfig struct {
	// Servers is a list of MCP server configurations
	Servers []MCPServerConfig `json:"servers,omitempty" yaml:"servers,omitempty" toml:"servers,omitempty"`
}

// MCPServerConfig represents configuration for a single MCP server connection.
// It supports multiple transport types and provides fine-grained control over
// connection behavior, tool filtering, and error handling.
type MCPServerConfig struct {
	// Name is a unique identifier for this server configuration
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`

	// Transport specifies the connection type: "stdio", "streamable_http", or "sse"
	Transport string `json:"transport,omitempty" yaml:"transport,omitempty" toml:"transport,omitempty"`

	// ServerURL is the HTTP/SSE server URL (required for streamable_http and sse transports)
	ServerURL string `json:"server_url,omitempty" yaml:"server_url,omitempty" toml:"server_url,omitempty"`

	// Command is the executable command (required for stdio transport)
	Command string `json:"command,omitempty" yaml:"command,omitempty" toml:"command,omitempty"`

	// Args are the command arguments (optional for stdio transport)
	Args []string `json:"args,omitempty" yaml:"args,omitempty" toml:"args,omitempty"`

	// Timeout is the connection timeout duration (default: 60s)
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" toml:"timeout,omitempty"`

	// Headers are custom HTTP headers (optional for HTTP/SSE transports)
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty" toml:"headers,omitempty"`

	// Env is environment variables for the MCP subprocess (stdio transport only).
	// Values support ${VAR} expansion from config. Use for tokens, e.g.
	// GITHUB_PERSONAL_ACCESS_TOKEN = "${GH_TOKEN}" with GH_TOKEN=$(gh auth token).
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty"`

	// IncludeTools is a list of tool names to include (allowlist)
	// If specified, only these tools will be available
	IncludeTools []string `json:"include_tools,omitempty" yaml:"include_tools,omitempty" toml:"include_tools,omitempty"`

	// ExcludeTools is a list of tool names to exclude (blocklist)
	// If specified, these tools will be filtered out
	ExcludeTools []string `json:"exclude_tools,omitempty" yaml:"exclude_tools,omitempty" toml:"exclude_tools,omitempty"`

	// SessionReconnect enables automatic session reconnection with max retry attempts
	// Set to 0 to disable session reconnection
	SessionReconnect int `json:"session_reconnect,omitempty" yaml:"session_reconnect,omitempty" toml:"session_reconnect,omitempty,omitzero"`

	// Auth configures credentials and authentication flows for this server.
	// Optional; if omitted, no auth is attempted (relies on Env or no auth).
	Auth *MCPAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty" toml:"auth,omitempty"`
}

// MCPAuthConfig configures how credentials are obtained.
type MCPAuthConfig struct {
	// Mode determines the auth type: "static", "oauth", or "mcp_oauth" (DCR).
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty" toml:"mode,omitempty"`
	// Provider is the goth provider name for "oauth" mode (e.g. "github", "google").
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty" toml:"provider,omitempty"`
	// ClientID is the OAuth client ID for "oauth" mode.
	ClientID string `json:"client_id,omitempty" yaml:"client_id,omitempty" toml:"client_id,omitempty"`
	// ClientSecret is the OAuth client secret for "oauth" mode. Supports ${VAR} expansion.
	ClientSecret string `json:"client_secret,omitempty" yaml:"client_secret,omitempty" toml:"client_secret,omitempty"`
	// Scopes are the OAuth scopes requested.
	Scopes []string `json:"scopes,omitempty" yaml:"scopes,omitempty" toml:"scopes,omitempty"`
	// TokenEnv is the secret name for "static" mode to lookup (e.g. "GITHUB_TOKEN").
	TokenEnv string `json:"token_env,omitempty" yaml:"token_env,omitempty" toml:"token_env,omitempty"`
}

// Validate validates the MCP configuration and returns an error if invalid.
// This ensures that all required fields are present and values are within acceptable ranges.
func (c *MCPConfig) Validate() error {
	if len(c.Servers) == 0 {
		return nil // Empty config is valid
	}

	serverNames := make(map[string]bool)
	for i, server := range c.Servers {
		if err := server.Validate(); err != nil {
			return fmt.Errorf("server[%d] validation failed: %w", i, err)
		}

		if serverNames[server.Name] {
			return fmt.Errorf("duplicate server name: %s", server.Name)
		}
		serverNames[server.Name] = true
	}

	return nil
}

// Validate validates a single MCP server configuration.
// It checks that required fields are present based on transport type and
// that values are within acceptable ranges.
func (s *MCPServerConfig) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("server name is required")
	}

	if s.Transport == "" {
		return fmt.Errorf("transport is required")
	}

	switch s.Transport {
	case "stdio":
		if s.Command == "" {
			return fmt.Errorf("command is required for stdio transport")
		}
	case "streamable_http", "sse":
		if s.ServerURL == "" {
			return fmt.Errorf("server_url is required for %s transport", s.Transport)
		}
	default:
		return fmt.Errorf("invalid transport: %s (must be stdio, streamable_http, or sse)", s.Transport)
	}

	return nil
}

// SetDefaults sets default values for the MCP configuration.
// This ensures that optional fields have sensible defaults when not specified.
func (s *MCPServerConfig) SetDefaults() {
	if s.Timeout == 0 {
		s.Timeout = 60 * time.Second
	}
}
