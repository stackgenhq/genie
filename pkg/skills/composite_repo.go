// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package skills

import (
	"errors"
	"os"
	"sort"

	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// CompositeRepository merges multiple skill repositories into a single source.
// This allows Genie to expose local filesystem scripts alongside remote MCP Prompts.
type CompositeRepository struct {
	repos []skill.Repository
}

// NewCompositeRepository creates a new repository that cascades across the given repos.
func NewCompositeRepository(repos ...skill.Repository) *CompositeRepository {
	// Filter out nil repositories just in case
	var active []skill.Repository
	for _, r := range repos {
		if r != nil {
			active = append(active, r)
		}
	}
	return &CompositeRepository{repos: active}
}

// Summaries returns the merged and sorted list of skill summaries from all underlying repositories.
func (c *CompositeRepository) Summaries() []skill.Summary {
	var all []skill.Summary
	seen := make(map[string]struct{})

	for _, repo := range c.repos {
		for _, s := range repo.Summaries() {
			if _, exists := seen[s.Name]; !exists {
				seen[s.Name] = struct{}{}
				all = append(all, s)
			}
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})

	return all
}

// Get iterates through the repositories and returns the first matching Skill.
func (c *CompositeRepository) Get(name string) (*skill.Skill, error) {
	for _, repo := range c.repos {
		if s, err := repo.Get(name); err == nil {
			return s, nil
		}
	}
	return nil, os.ErrNotExist
}

// Path iterates through the repositories and returns the first successful Path resolution.
func (c *CompositeRepository) Path(name string) (string, error) {
	var lastErr error
	for _, repo := range c.repos {
		p, err := repo.Path(name)
		if err == nil {
			return p, nil
		}
		// Some repositories (like MCP Prompts) don't have paths and might return os.ErrNotExist.
		if !errors.Is(err, os.ErrNotExist) {
			lastErr = err
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", os.ErrNotExist
}
