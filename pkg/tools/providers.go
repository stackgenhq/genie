// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/skills"
	"github.com/stackgenhq/genie/pkg/tools/skills/dynamicskills"
	"github.com/stackgenhq/genie/pkg/tools/unix"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	ctxtools "trpc.group/trpc-go/trpc-agent-go/tool/context"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"

	skilltool "trpc.group/trpc-go/trpc-agent-go/tool/skill"
)

//go:generate go tool counterfeiter -generate

//counterfeiter:generate trpc.group/trpc-go/trpc-agent-go/tool.CallableTool

// Slice is a convenience adapter that wraps a plain []tool.Tool into a
// ToolProviders conformer. Useful for ad-hoc tool collections that don't
// belong to a dedicated package (e.g. the codeowner's orchestration-only
// tool set).
type Tools []tool.Tool

// GetTools returns the wrapped slice of tools.
func (s Tools) GetTools() []tool.Tool { return s }

// FileToolProvider wraps a trpc-agent-go file.ToolSet and satisfies the
// ToolProviders interface. The tools are pre-computed at construction time
// because file.ToolSet.Tools requires a context.
type FileToolProvider struct {
	tools Tools
}

// NewFileToolProvider creates a ToolProvider for file tools scoped to a
// working directory. Returns nil if the ToolSet fails to initialise.
func NewFileToolProvider(ctx context.Context, workingDir string) *FileToolProvider {
	ts, err := file.NewToolSet(file.WithBaseDir(workingDir))
	if err != nil {
		return nil
	}
	return &FileToolProvider{tools: ts.Tools(ctx)}
}

// GetTools returns the pre-computed file tools.
func (p *FileToolProvider) GetTools() []tool.Tool {
	return p.tools
}

// ShellToolProvider wraps the shell tool and satisfies the ToolProviders
// interface. It encapsulates code executor configuration.
type ShellToolProvider struct {
	workingDir string
	secrets    security.SecretProvider
	config     unix.ShellToolConfig
}

// NewShellToolProvider creates a ToolProvider for the run_shell tool.
// The config controls security features like env var filtering and binary allowlists.
// The SecretProvider is used to resolve allowed environment variables at runtime.
func NewShellToolProvider(workingDir string, secrets security.SecretProvider, config unix.ShellToolConfig) *ShellToolProvider {
	return &ShellToolProvider{
		workingDir: workingDir,
		secrets:    secrets,
		config:     config,
	}
}

// GetTools returns the shell tool backed by a local code executor.
func (p *ShellToolProvider) GetTools() []tool.Tool {
	timeout := p.config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	exec := local.New(
		local.WithWorkDir(p.workingDir),
		local.WithTimeout(timeout),
		local.WithCleanTempFiles(true),
	)
	return Tools{unix.NewShellTool(exec, p.secrets, p.config)}
}

// PensieveToolProvider wraps the Pensieve context management tools
// (delete_context, check_budget, note, read_notes) and satisfies the
// ToolProviders interface. Gated behind EnablePensieve in config.
type PensieveToolProvider struct{}

// NewPensieveToolProvider creates a ToolProvider for the Pensieve tools.
func NewPensieveToolProvider() *PensieveToolProvider {
	return &PensieveToolProvider{}
}

// GetTools returns the context management tools.
func (p *PensieveToolProvider) GetTools() []tool.Tool {
	return ctxtools.Tools()
}

type SkillLoadConfig struct {
	MaxLoadedSkills int      `yaml:"max_loaded_skills,omitempty" toml:"max_loaded_skills,omitempty"`
	SkillsRoots     []string `yaml:"skills_roots,omitempty" toml:"skills_roots,omitempty"`
}

func DefaultSkillLoadConfig() SkillLoadConfig {
	return SkillLoadConfig{
		MaxLoadedSkills: 3,
	}
}

// SkillToolProvider wraps the skill loading tools (skill_list_docs, skill_load, skill_run).
type SkillToolProvider struct {
	repo   skill.Repository
	exec   codeexecutor.CodeExecutor
	loader *dynamicskills.DynamicSkillLoader
}

// NewSkillToolProvider creates a ToolProvider containing skill discovery tools.
func NewSkillToolProvider(workingDir string, config SkillLoadConfig, additionalRepos ...skill.Repository) (*SkillToolProvider, error) {
	fsRepo, err := skill.NewFSRepository(config.SkillsRoots...)
	if err != nil {
		return nil, fmt.Errorf("error creating skill repository: %w", err)
	}
	reposToMerge := []skill.Repository{fsRepo}
	reposToMerge = append(reposToMerge, additionalRepos...)
	repo := skills.NewCompositeRepository(reposToMerge...)
	exec := local.New(
		local.WithWorkDir(workingDir),
		local.WithTimeout(10*time.Minute),
		local.WithCleanTempFiles(true),
	)

	p := &SkillToolProvider{repo: repo, exec: exec}
	// The provider implements SkillRegistry natively, so it passes itself as the registry
	p.loader = dynamicskills.NewDynamicSkillLoader(p, config.MaxLoadedSkills)

	return p, nil
}

// Clone returns a new SkillToolProvider with a fresh DynamicSkillLoader (empty
// loaded-skills state) but sharing the same skill repository and code executor.
// This implements CloneableToolProvider so that CloneWithEphemeralProviders()
// can isolate dynamic-skill state per sub-agent — without this, skill
// load/unload state would be shared across all sub-agents.
func (p *SkillToolProvider) Clone() ToolProviders {
	cloned := &SkillToolProvider{repo: p.repo, exec: p.exec}
	cloned.loader = dynamicskills.NewDynamicSkillLoader(cloned, p.loader.MaxSkills())
	return cloned
}

// GetTools returns the tools needed for agents to dynamically discover and load skills.
func (p *SkillToolProvider) GetTools() []tool.Tool {
	tools := []tool.Tool{
		dynamicskills.DiscoverSkillsTool(p.loader.Registry()),
		dynamicskills.LoadSkillTool(p.loader),
		dynamicskills.UnloadSkillTool(p.loader),
	}

	// Add currently loaded dynamic skills
	tools = append(tools, p.loader.GetLoadedTools()...)
	return tools
}

// Search implements dynamicskills.SkillRegistry.
func (p *SkillToolProvider) Search(query string) []dynamicskills.Skill {
	// If query is empty, we return all skills.
	// We rely on the trpc-agent-go skill repository for list.
	summaries := p.repo.Summaries()
	var results []dynamicskills.Skill
	for _, summary := range summaries {
		if query != "" && !strings.Contains(strings.ToLower(summary.Name), strings.ToLower(query)) && !strings.Contains(strings.ToLower(summary.Description), strings.ToLower(query)) {
			continue
		}

		// trpc-agent-go skills are invoked via skill_run which takes a skill name as parameter.
		// For dynamic skills, we give them direct access to skill_run.
		var runTool tool.Tool = skilltool.NewRunTool(p.repo, p.exec)
		if callable, ok := runTool.(tool.CallableTool); ok {
			runTool = &restrictedSkillRunTool{CallableTool: callable, loader: p.loader}
		}

		results = append(results, dynamicskills.Skill{
			Name:        summary.Name,
			Description: summary.Description,
			Tools:       []tool.Tool{runTool}, // Alternatively, wrap RunTool to preset the skill parameter
		})
	}
	return results
}

// Get implements dynamicskills.SkillRegistry.
func (p *SkillToolProvider) Get(name string) (dynamicskills.Skill, bool) {
	skillData, err := p.repo.Get(name)
	if err != nil {
		return dynamicskills.Skill{}, false
	}

	var runTool tool.Tool = skilltool.NewRunTool(p.repo, p.exec)
	if callable, ok := runTool.(tool.CallableTool); ok {
		runTool = &restrictedSkillRunTool{CallableTool: callable, loader: p.loader}
	}
	return dynamicskills.Skill{
		Name:        skillData.Summary.Name,
		Description: skillData.Summary.Description,
		Tools:       []tool.Tool{runTool},
	}, true
}

type restrictedSkillRunTool struct {
	tool.CallableTool
	loader *dynamicskills.DynamicSkillLoader
}

func (r *restrictedSkillRunTool) Call(ctx context.Context, args []byte) (any, error) {
	var parsed struct {
		SkillName string `json:"skill_name"`
	}
	if err := json.Unmarshal(args, &parsed); err == nil && parsed.SkillName != "" {
		if !r.loader.IsLoaded(parsed.SkillName) {
			return nil, fmt.Errorf("skill %q is not currently loaded. You must load it using load_skill first", parsed.SkillName)
		}
	}
	return r.CallableTool.Call(ctx, args)
}
