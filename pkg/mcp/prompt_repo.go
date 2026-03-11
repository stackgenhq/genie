// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// MCPPromptCaller is an interface for fetching prompts from an MCP server.
// Extracted to allow unit testing with counterfeiter fakes.
//
//go:generate go tool counterfeiter -generate
//counterfeiter:generate . MCPPromptCaller
type MCPPromptCaller interface {
	GetPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
}

// PromptRepository exposes MCP Prompts as Genie Skills.
// It implements the trpc.group/trpc-go/trpc-agent-go/skill.Repository interface.
type PromptRepository struct {
	client *Client
}

// NewPromptRepository creates a new PromptRepository wrapping an MCP Client.
func NewPromptRepository(client *Client) *PromptRepository {
	return &PromptRepository{
		client: client,
	}
}

// Summaries returns all available prompt summaries from all connected MCP servers.
// To avoid namespace collisions, the skill name is prefixed with the server name.
func (r *PromptRepository) Summaries() []skill.Summary {
	var summaries []skill.Summary

	if r.client == nil {
		return summaries
	}

	for _, p := range r.client.prompts {
		summaries = append(summaries, skill.Summary{
			Name:        p.serverName + "_" + p.prompt.Name,
			Description: p.prompt.Description,
		})
	}

	return summaries
}

// Get fetches a specific prompt from the remote MCP server and maps it to a Skill.
func (r *PromptRepository) Get(name string) (*skill.Skill, error) {
	if r.client == nil {
		return nil, fmt.Errorf("prompt_repo: client is nil")
	}

	// Find the matching prompt to know which server it belongs to
	var targetPrompt *namespacedPrompt
	var targetClient MCPPromptCaller

	for i := range r.client.prompts {
		p := &r.client.prompts[i]
		if p.serverName+"_"+p.prompt.Name == name {
			targetPrompt = p
			targetClient = p.caller
			break
		}
	}

	if targetPrompt == nil {
		return nil, fmt.Errorf("skill/prompt %q not found", name)
	}

	// Fetch the prompt content from the server
	req := mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name:      targetPrompt.prompt.Name,
			Arguments: make(map[string]string), // Standard arg-less fetch for skills
		},
	}

	resp, err := targetClient.GetPrompt(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt from server %s: %w", targetPrompt.serverName, err)
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
			Description: targetPrompt.prompt.Description,
		},
		Body: strings.TrimSpace(body),
	}, nil
}

// Path is not completely applicable to MCP Prompts as they are remote.
// Expected usage is for scripts that need to be run, so we return an error.
func (r *PromptRepository) Path(name string) (string, error) {
	return "", os.ErrNotExist
}
