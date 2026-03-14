// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

//go:generate go tool counterfeiter -generate

// MCPPromptCaller is an interface for fetching prompts from an MCP server.
// Extracted to allow unit testing with counterfeiter fakes.
//
//counterfeiter:generate . MCPPromptCaller
type MCPPromptCaller interface {
	GetPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
	ListPrompts(ctx context.Context, req mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error)
	io.Closer
}

// promptDialer abstracts ephemeral MCP client creation so tests can inject a
// fake without hitting real transports.
type promptDialer func(ctx context.Context, cfg MCPServerConfig, sp security.SecretProvider) (MCPPromptCaller, error)

// defaultPromptDialer wraps dialMCP and returns the concrete *client.Client
// which satisfies MCPPromptCaller (GetPrompt + ListPrompts + Close).
func defaultPromptDialer(ctx context.Context, cfg MCPServerConfig, sp security.SecretProvider) (MCPPromptCaller, error) {
	return dialMCP(ctx, cfg, sp)
}

// Compile-time check that *client.Client satisfies MCPPromptCaller.
var _ MCPPromptCaller = (*mcpclient.Client)(nil)

// PromptRepository exposes MCP Prompts as Genie Skills.
// It implements the trpc.group/trpc-go/trpc-agent-go/skill.Repository interface.
// Each call to Summaries or Get creates an ephemeral MCP client connection to
// the configured server, fetches the data, and closes the connection.
type PromptRepository struct {
	cfg            MCPServerConfig
	secretProvider security.SecretProvider
	dial           promptDialer
}

// NewPromptRepository creates a new PromptRepository for the given MCP server config.
// secretProvider is optional; when set it is used to resolve ${VAR} references in
// stdio env values and HTTP headers.
func NewPromptRepository(cfg MCPServerConfig, sp security.SecretProvider) *PromptRepository {
	return &PromptRepository{
		cfg:            cfg,
		secretProvider: sp,
		dial:           defaultPromptDialer,
	}
}

// Summaries connects to the MCP server, lists all prompts, and returns them as
// skill.Summary entries. The skill name is prefixed with the server name to
// avoid namespace collisions.
func (r *PromptRepository) Summaries() []skill.Summary {
	var summaries []skill.Summary

	ctx := context.Background()
	caller, err := r.dial(ctx, r.cfg, r.secretProvider)
	if err != nil {
		logger.GetLogger(ctx).With("fn", "PromptRepository.Summaries").
			Warn("failed to connect to MCP server", "server", r.cfg.Name, "error", err)
		return summaries
	}
	defer caller.Close()

	resp, err := caller.ListPrompts(ctx, mcp.ListPromptsRequest{})
	if err != nil {
		logger.GetLogger(ctx).With("fn", "PromptRepository.Summaries").
			Warn("failed to list prompts", "server", r.cfg.Name, "error", err)
		return summaries
	}

	for _, p := range resp.Prompts {
		summaries = append(summaries, skill.Summary{
			Name:        r.cfg.Name + "_" + p.Name,
			Description: p.Description,
		})
	}

	return summaries
}

// Get connects to the MCP server, fetches a specific prompt by name, and maps
// it to a Skill.
func (r *PromptRepository) Get(name string) (*skill.Skill, error) {
	// Strip the server name prefix to get the raw prompt name.
	prefix := r.cfg.Name + "_"
	if !strings.HasPrefix(name, prefix) {
		return nil, fmt.Errorf("skill/prompt %q not found (expected prefix %q)", name, prefix)
	}
	promptName := strings.TrimPrefix(name, prefix)

	ctx := context.Background()
	caller, err := r.dial(ctx, r.cfg, r.secretProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server %s: %w", r.cfg.Name, err)
	}
	defer caller.Close()

	req := mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name:      promptName,
			Arguments: make(map[string]string),
		},
	}

	resp, err := caller.GetPrompt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt from server %s: %w", r.cfg.Name, err)
	}

	// Convert content to string body
	var body string
	if resp.Description != "" {
		body += resp.Description + "\n\n"
	}
	for _, content := range resp.Messages {
		if textContent, ok := content.Content.(mcp.TextContent); ok {
			body += textContent.Text + "\n"
		} else if imgContent, ok := content.Content.(mcp.ImageContent); ok {
			body += fmt.Sprintf("[Image: %s]\n", imgContent.Data)
		}
	}

	return &skill.Skill{
		Summary: skill.Summary{
			Name:        name,
			Description: resp.Description,
		},
		Body: strings.TrimSpace(body),
	}, nil
}

// Path is not applicable to MCP Prompts as they are remote.
func (r *PromptRepository) Path(name string) (string, error) {
	return "", os.ErrNotExist
}
