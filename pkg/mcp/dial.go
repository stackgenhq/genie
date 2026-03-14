// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
)

// dialMCP creates an ephemeral MCP client connection to the given server.
// The caller must close the returned client when finished.
func dialMCP(ctx context.Context, cfg MCPServerConfig, sp security.SecretProvider) (*client.Client, error) {
	var trans transport.Interface
	var err error

	switch cfg.Transport {
	case "stdio":
		env := buildStdioEnvFromConfig(ctx, cfg, sp)
		trans = transport.NewStdio(cfg.Command, env, cfg.Args...)
	case "streamable_http", "sse":
		var opts []transport.ClientOption
		if len(cfg.Headers) > 0 {
			headers := make(map[string]string, len(cfg.Headers))
			for k, v := range cfg.Headers {
				if sp != nil && strings.Contains(v, "$") {
					v = expandEnvWithProvider(ctx, v, sp)
				}
				headers[k] = v
			}
			opts = append(opts, transport.WithHeaders(headers))
		}
		trans, err = transport.NewSSE(cfg.ServerURL, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSE transport: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported transport: %s", cfg.Transport)
	}

	mcpClient := client.NewClient(trans)

	if err = mcpClient.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start MCP transport: %w", err)
	}

	if _, err = mcpClient.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		mcpClient.Close()
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	return mcpClient, nil
}

// buildStdioEnvFromConfig returns the environment for a stdio subprocess:
// parent env plus config.Env overrides. When sp is set, any value containing
// "${" is resolved via the provider.
func buildStdioEnvFromConfig(ctx context.Context, cfg MCPServerConfig, sp security.SecretProvider) []string {
	base := os.Environ()
	if len(cfg.Env) == 0 {
		return base
	}
	overlay := make(map[string]string)
	for _, s := range base {
		if i := strings.IndexByte(s, '='); i > 0 {
			overlay[s[:i]] = s[i+1:]
		}
	}
	for k, v := range cfg.Env {
		if sp != nil && strings.Contains(v, "${") {
			v = expandEnvWithProvider(ctx, v, sp)
		}
		overlay[k] = v
	}
	out := make([]string, 0, len(overlay))
	for k, v := range overlay {
		out = append(out, k+"="+v)
	}
	return out
}

// expandEnvWithProvider expands ${NAME} and $NAME in value using the given
// SecretProvider.
func expandEnvWithProvider(ctx context.Context, value string, sp security.SecretProvider) string {
	return os.Expand(value, func(name string) string {
		val, err := sp.GetSecret(ctx, security.GetSecretRequest{
			Name:   name,
			Reason: toolcontext.GetJustification(ctx),
		})
		if err != nil {
			logger.GetLogger(ctx).With("fn", "mcp.expandEnvWithProvider").Debug("secret lookup failed", "name", name, "error", err)
			return ""
		}
		return val
	})
}
