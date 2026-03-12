// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package executable provides a generic tool wrapper for arbitrary binaries.
package executable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/shlex"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Configs represents a list of executable tool configurations.
type Configs []Config

// Config holds the configuration for a generic executable tool.
type Config struct {
	Name        string   `yaml:"name" toml:"name"`
	Binary      string   `yaml:"binary" toml:"binary"`
	Description string   `yaml:"description" toml:"description"`
	Env         []EnvVar `yaml:"env,omitempty" toml:"env,omitempty"`
}

// EnvVar represents an environment variable key-value pair, or a lookup.
type EnvVar struct {
	Key    string `yaml:"key" toml:"key"`
	Value  string `yaml:"value,omitempty" toml:"value,omitempty"`
	Secret string `yaml:"secret,omitempty" toml:"secret,omitempty"`
}

// Tools resolves secrets and validates binary availability, converting
// valid configurations into tool.Tool instances.
func (c Configs) Tools(ctx context.Context, sp security.SecretProvider) []tool.Tool {
	log := logger.GetLogger(ctx).With("fn", "executable.Tools")
	var tools []tool.Tool

	for _, cfg := range c {
		if t, err := cfg.Tool(ctx, sp); err != nil {
			log.Warn("executable tool skipped", "name", cfg.Name, "error", err)
		} else if t != nil {
			log.Info("executable tool initialized", "name", cfg.Name, "binary", cfg.Binary)
			tools = append(tools, t)
		}
	}

	return tools
}

// Tool validates the configuration and returns an executable tool.
func (cfg Config) Tool(ctx context.Context, sp security.SecretProvider) (tool.Tool, error) {
	if cfg.Name == "" || cfg.Binary == "" {
		return nil, fmt.Errorf("missing name or binary")
	}

	if _, err := exec.LookPath(cfg.Binary); err != nil {
		return nil, fmt.Errorf("binary not found on PATH: %w", err)
	}

	for _, e := range cfg.Env {
		if e.Secret != "" {
			val, err := sp.GetSecret(ctx, security.GetSecretRequest{
				Name:   e.Secret,
				Reason: "tool secret validation",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to validate secret %q for env var %q: %w", e.Secret, e.Key, err)
			}
			if val == "" {
				return nil, fmt.Errorf("missing required secret %q for env var %q", e.Secret, e.Key)
			}
		}
	}

	return &executableTool{cfg: cfg, sp: sp}, nil
}

// resolveEnv iterates through configured environment variables and performs secret lookups.
func (cfg Config) resolveEnv(ctx context.Context, sp security.SecretProvider) ([]string, error) {
	var env []string
	for _, e := range cfg.Env {
		val := e.Value
		if e.Secret != "" {
			v, err := sp.GetSecret(ctx, security.GetSecretRequest{
				Name:   e.Secret,
				Reason: toolcontext.GetJustification(ctx),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to get secret %q for env var %q: %w", e.Secret, e.Key, err)
			}
			val = v
		}
		env = append(env, e.Key+"="+val)
	}
	return env, nil
}

type executableTool struct {
	cfg Config
	sp  security.SecretProvider
}

// Declaration returns the tool metadata for the LLM.
func (t *executableTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        t.cfg.Name,
		Description: t.cfg.Description,
		InputSchema: &tool.Schema{
			Type:     "object",
			Required: []string{"command"},
			Properties: map[string]*tool.Schema{
				"command": {
					Type:        "string",
					Description: fmt.Sprintf("The command to execute, excluding the base binary '%s'. Example: 'subcommand --flag value'", t.cfg.Binary),
				},
			},
		},
	}
}

// Call executes the binary with the configured environment variables.
func (t *executableTool) Call(ctx context.Context, input []byte) (any, error) {
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
	// # and \ are included for defense-in-depth (comments, escape sequences).
	if strings.ContainsAny(args.Command, ";|&`$(){}!><\n#\\") {
		return nil, fmt.Errorf("command contains disallowed shell metacharacters — use simple arguments only (no pipes, redirects, or sub-shells)")
	}

	// Split into argv using shlex to respect embedded quotes and execute directly — no shell involved.
	argv, err := shlex.Split(args.Command)
	if err != nil {
		return nil, fmt.Errorf("failed to split arguments: %w", err)
	}
	cmd := exec.CommandContext(ctx, t.cfg.Binary, argv...)

	env, err := t.cfg.resolveEnv(ctx, t.sp)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve env: %w", err)
	}

	// Build a minimal environment for the subprocess. Only expose
	// PATH, HOME, and explicitly configured env vars.
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"NO_COLOR=1", // disable ANSI colours in output for easier parsing
	}, env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s command failed: %w\noutput: %s", t.cfg.Binary, err, string(output))
	}

	return string(output), nil
}
