// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// LoadSkillsFromConfig loads skills from the configuration.
// This function exists to initialize the skills system based on the Genie configuration.
// Without this function, we could not load skills from the config file.
// It supports multiple skills roots including local paths and remote HTTPS URLs.
// It also accepts additional repositories (like an MCP PromptRepository) to be merged.
func LoadSkillsFromConfig(ctx context.Context, roots []string, additionalRepos ...skill.Repository) ([]tool.Tool, error) {
	logr := logger.GetLogger(ctx).With("fn", "LoadSkillsFromConfig")

	// If no skills roots configured and no additional repos, return empty tools list
	if len(roots) == 0 && len(additionalRepos) == 0 {
		logr.Debug("no skills roots or additional repos configured, skills disabled")
		return nil, nil
	}

	// Process and validate each root
	var validRoots []string
	for _, root := range roots {
		// Expand environment variables
		expandedRoot := os.ExpandEnv(root)

		// Check if it's a remote HTTPS URL
		if strings.HasPrefix(expandedRoot, "https://") || strings.HasPrefix(expandedRoot, "http://") {
			logr.Info("adding remote skills root", "url", expandedRoot)
			validRoots = append(validRoots, expandedRoot)
			continue
		}

		// For local paths, resolve to absolute path and validate
		absPath, err := filepath.Abs(expandedRoot)
		if err != nil {
			logr.Warn("failed to resolve skills path, skipping", "path", expandedRoot, "error", err)
			continue
		}

		// Check if directory exists
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				logr.Warn("skills directory does not exist, skipping", "path", absPath)
				continue
			}
			logr.Warn("failed to stat skills directory, skipping", "path", absPath, "error", err)
			continue
		}
		if !info.IsDir() {
			logr.Warn("skills path is not a directory, skipping", "path", absPath)
			continue
		}

		logr.Info("adding local skills root", "path", absPath)
		validRoots = append(validRoots, absPath)
	}

	if len(validRoots) == 0 && len(additionalRepos) == 0 {
		logr.Warn("no valid skills roots or additional repos found, skills disabled")
		return nil, nil
	}

	var reposToMerge []skill.Repository
	if len(validRoots) > 0 {
		// Create skill repository using trpc-agent-go/skill package with multiple roots
		fsRepo, err := skill.NewFSRepository(validRoots...)
		if err != nil {
			return nil, fmt.Errorf("failed to create skills repository: %w", err)
		}
		reposToMerge = append(reposToMerge, fsRepo)
	}

	reposToMerge = append(reposToMerge, additionalRepos...)
	
	// Create the composite repository
	repo := NewCompositeRepository(reposToMerge...)

	// Log discovered skills
	summaries := repo.Summaries()
	logr.Info("skills loaded", "count", len(summaries), "roots", len(validRoots))
	for _, s := range summaries {
		logr.Debug("discovered skill", "name", s.Name, "description", s.Description)
	}

	// Create executor with workspace in temp directory
	workDir := filepath.Join(os.TempDir(), "genie-skills")
	executor := NewLocalExecutor(workDir)

	// Create and return skill tools
	tools := CreateAllSkillTools(repo, executor)
	logr.Info("skill tools created", "count", len(tools))

	return tools, nil
}
