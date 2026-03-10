// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package ghcli provides a GitHub CLI (gh) tool that agents can use
// for GitHub operations not covered by the native SCM tools, such as
// viewing workflow run logs, listing Actions runs, and other gh-specific
// capabilities.
//
// The tool is only bootstrapped when both the `gh` binary is available
// on PATH and a GITHUB_TOKEN is provided via the secret configuration.
// This keeps the tool optional — deployments without GitHub access
// simply skip it.
package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/shlex"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Config holds configuration for the gh CLI tool provider.
// Token is the GitHub personal access token or fine-grained token.
// When empty, the tool provider is not bootstrapped.
type Config struct {
	Token string `yaml:"token,omitempty" toml:"token,omitempty"`
}

// ghCLITool wraps the gh CLI binary as an agent tool.
// It authenticates via GH_TOKEN and GITHUB_TOKEN environment variable injection
// per-invocation (never written to disk or shell config).
type ghCLITool struct {
	token string
}

// Declaration returns the tool metadata for the LLM.
func (t *ghCLITool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name: "gh_cli",
		Description: `Execute a GitHub CLI (gh) command. Use this for GitHub-specific operations 
not covered by native SCM tools, such as:
- Viewing workflow run logs: gh run view <id> --log-failed
- Listing Actions runs: gh run list --repo owner/repo
- Inspecting deployments: gh api repos/owner/repo/deployments
- Branch protection audits: gh api repos/owner/repo/branches/main/protection
- Dependabot alerts: gh api repos/owner/repo/dependabot/alerts

The gh CLI is pre-authenticated. Pass the full gh command (without the leading 'gh' binary name).
Example: command="run list --repo owner/repo --status failure --limit 10"`,
		InputSchema: &tool.Schema{
			Type:     "object",
			Required: []string{"command"},
			Properties: map[string]*tool.Schema{
				"command": {
					Type:        "string",
					Description: "The gh CLI command to execute (without the leading 'gh'). Example: 'run list --repo owner/repo --status failure'",
				},
			},
		},
	}
}

// Call executes the gh CLI command with the configured token.
// The token is passed via both GH_TOKEN and GITHUB_TOKEN environment variables for the
// subprocess only — it is never persisted to disk or leaked to other
// processes.
//
// Security: the command string is split into argv tokens using strings.Fields
// and passed directly to exec.CommandContext (no shell). Shell metacharacters
// (pipes, semi-colons, backticks, etc.) are rejected to prevent injection.
func (t *ghCLITool) Call(ctx context.Context, input []byte) (any, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if strings.TrimSpace(args.Command) == "" {
		return nil, fmt.Errorf("command is required")
	}

	// Reject shell metacharacters to prevent injection.
	// The gh CLI should never need pipes, redirects, or sub-shells.
	// # and \ are included for defense-in-depth (comments, escape sequences).
	if strings.ContainsAny(args.Command, ";|&`$(){}!><\n#\\") {
		return nil, fmt.Errorf("command contains disallowed shell metacharacters — use simple gh arguments only (no pipes, redirects, or sub-shells)")
	}

	// Split into argv using shlex to respect embedded quotes and execute gh directly — no shell involved.
	argv, err := shlex.Split(args.Command)
	if err != nil {
		return nil, fmt.Errorf("failed to split arguments: %w", err)
	}
	cmd := exec.CommandContext(ctx, "gh", argv...)

	// Build a minimal environment for the gh subprocess. Only expose
	// PATH (command resolution), HOME (config dir), and the auth tokens.
	// This prevents leaking sensitive host env vars (e.g. POSTGRES_DSN,
	// AWS_SECRET_ACCESS_KEY) to the subprocess — same isolation principle
	// as shell_tool's env -i approach.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GH_TOKEN=" + t.token,
		"GITHUB_TOKEN=" + t.token,
		"GH_HOST=github.com",
		"NO_COLOR=1", // disable ANSI colours in output for easier parsing
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh command failed: %w\noutput: %s", err, string(output))
	}

	return string(output), nil
}

// ToolProvider wraps the gh CLI tool and satisfies the tools.ToolProviders
// interface so it can be passed directly to tools.NewRegistry.
// The provider is only created when gh is available and a token is configured.
type ToolProvider struct {
	tool *ghCLITool
}

// New creates a ToolProvider if both the gh binary is on PATH and the
// token is non-empty. Returns nil if either prerequisite is missing.
// This makes the tool entirely opt-in: deployments without the gh binary
// or without a GITHUB_TOKEN simply don't get the tool.
func New(ctx context.Context, cfg Config) *ToolProvider {
	log := logger.GetLogger(ctx).With("fn", "ghcli.New")

	if cfg.Token == "" {
		log.Debug("gh CLI tool skipped: no GITHUB_TOKEN configured")
		return nil
	}

	if _, err := exec.LookPath("gh"); err != nil {
		log.Debug("gh CLI tool skipped: gh binary not found on PATH")
		return nil
	}

	log.Info("gh CLI tool provider bootstrapped")
	return &ToolProvider{
		tool: &ghCLITool{token: cfg.Token},
	}
}

// GetTools returns the gh_cli tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{p.tool}
}
