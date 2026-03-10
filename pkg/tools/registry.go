// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProviders is the interface that all tool-producing types must satisfy
// in order to be passed to NewRegistry. Each provider is responsible for
// constructing its own tools from whatever dependencies it holds.
type ToolProviders interface {
	GetTools() []tool.Tool
}

// CloneableToolProvider represents a provider with epigenetic context that
// should be instantiated freshly for isolated environments (like sub-agents).
type CloneableToolProvider interface {
	ToolProviders
	Clone() ToolProviders
}

// Registry holds the bootstrapped, filtered set of tools.
// It evaluates its providers dynamically so that dynamically loaded skills
// become instantly available in downstream agents without needing a restart.
type Registry struct {
	providers []ToolProviders
	hasCfg    bool
	cfg       hitl.Config
	includes  map[string]struct{}
	excludes  map[string]struct{}
}

// NewRegistry collects tools from every supplied ToolProviders conformer
// and evaluates them dynamically on each GetTools() call.
// The returned Registry provides the aggregated tool list via AllTools().
// Use FilterDenied to subsequently exclude tools blocked by HITL config.
func NewRegistry(ctx context.Context, providers ...ToolProviders) *Registry {
	log := logger.GetLogger(ctx).With("fn", "toolreg.NewRegistry")

	r := &Registry{
		providers: providers,
	}

	log.Info("Tool registry initialized", "total", len(r.getToolsMap()))

	return r
}

// getToolsMap rebuilds the aggregated tool map by calling each provider's
// GetTools() on every access. This is intentionally uncached so that dynamic
// providers (e.g. SkillToolProvider) can reflect newly loaded skills instantly.
//
// IMPORTANT: This is a hot path — it is called on every GetTools/GetTool/
// ToolNames/etc. invocation. Provider.GetTools() implementations MUST be cheap
// and side-effect free (no allocations of new tool instances, no I/O).
func (r *Registry) getToolsMap() map[string]tool.Tool {
	raw := make(map[string]tool.Tool)
	for _, p := range r.providers {
		for _, t := range p.GetTools() {
			raw[t.Declaration().Name] = t
		}
	}

	filtered := make(map[string]tool.Tool)
	for name, t := range raw {
		if r.hasCfg && r.cfg.IsDenied(name) {
			continue
		}
		if r.excludes != nil {
			if _, skip := r.excludes[name]; skip {
				continue
			}
		}
		if r.includes != nil {
			if _, keep := r.includes[name]; !keep {
				continue
			}
		}
		filtered[name] = t
	}
	return filtered
}

func (r *Registry) GetTool(name string) (tool.Tool, error) {
	tools := r.getToolsMap()
	if t, ok := tools[name]; ok {
		return t, nil
	}
	return nil, errors.New("tool not found")
}

// FilterDenied returns a new Registry that excludes any tools denied by
// the HITL config. This is the single place where tool-level deny-listing
// is applied. Without this, denied tools would still be available to agents.
func (r *Registry) FilterDenied(ctx context.Context, cfg hitl.Config) *Registry {
	log := logger.GetLogger(ctx).With("fn", "toolreg.FilterDenied")

	r2 := r.clone()
	r2.cfg = cfg
	r2.hasCfg = true

	// Log metrics
	before := len(r.getToolsMap())
	after := len(r2.getToolsMap())
	log.Info("Tool registry filtered",
		"before", before,
		"after", after,
		"excluded", before-after)

	return r2
}

// GetTools returns the full set of available (non-denied) tools.
// Satisfies the ToolProviders interface so a Registry can be passed
// as a provider to another Registry (e.g. codeOwner tools).
func (r *Registry) GetTools() []tool.Tool {
	m := r.getToolsMap()
	tools := make([]tool.Tool, 0, len(m))
	for _, t := range m {
		tools = append(tools, t)
	}
	return tools
}

// AllTools is a convenience alias for GetTools. Prefer AllTools when
// calling from application code for readability; GetTools exists to
// satisfy the ToolProviders interface.
func (r *Registry) AllTools() Tools {
	return r.GetTools()
}

// ToolNames returns the names of all available tools (for logging).
func (r *Registry) ToolNames() []string {
	m := r.getToolsMap()
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}

func (r *Registry) GetToolDescriptions() []string {
	m := r.getToolsMap()
	descriptions := make([]string, 0, len(m))
	for _, t := range m {
		descriptions = append(descriptions, fmt.Sprintf("%s: %s", t.Declaration().Name, t.Declaration().Description))
	}
	sorted := make([]string, len(descriptions))
	copy(sorted, descriptions)
	sort.Strings(sorted)
	return sorted
}

func (r *Registry) clone() *Registry {
	var includes map[string]struct{}
	if r.includes != nil {
		includes = make(map[string]struct{})
		for k := range r.includes {
			includes[k] = struct{}{}
		}
	}
	var excludes map[string]struct{}
	if r.excludes != nil {
		excludes = make(map[string]struct{})
		for k := range r.excludes {
			excludes[k] = struct{}{}
		}
	}
	return &Registry{
		providers: r.providers,
		hasCfg:    r.hasCfg,
		cfg:       r.cfg,
		includes:  includes,
		excludes:  excludes,
	}
}

// CloneWithEphemeralProviders creates a copy of the Registry and evaluates Clone()
// on providers that implement CloneableToolProvider, guaranteeing epigenetic isolation.
// This is used to ensure sub-agents get a fresh, empty state for dynamically loaded skills.
func (r *Registry) CloneWithEphemeralProviders() *Registry {
	r2 := r.clone()
	var clonedProviders []ToolProviders
	for _, p := range r.providers {
		if cp, ok := p.(CloneableToolProvider); ok {
			clonedProviders = append(clonedProviders, cp.Clone())
		} else {
			clonedProviders = append(clonedProviders, p)
		}
	}
	r2.providers = clonedProviders
	return r2
}

// Exclude returns a new Registry that omits tools with the given names.
// Used to strip orchestration-only tools (e.g. create_agent, send_message)
// before passing the registry to sub-agents.
func (r *Registry) Exclude(names ...string) *Registry {
	if len(names) == 0 {
		return r
	}
	r2 := r.clone()
	if r2.excludes == nil {
		r2.excludes = make(map[string]struct{})
	}
	for _, n := range names {
		r2.excludes[n] = struct{}{}
	}
	return r2
}

// Include returns a new Registry containing only tools whose names appear
// in the provided list. Unknown names are silently ignored. If names is
// empty, the original Registry is returned unchanged (all tools available).
// Used to scope sub-agents to exactly the tools the planner selected.
func (r *Registry) Include(names ...string) *Registry {
	if len(names) == 0 {
		return r
	}
	r2 := r.clone()
	if r2.includes == nil {
		r2.includes = make(map[string]struct{})
	}
	for _, n := range names {
		r2.includes[n] = struct{}{}
	}
	return r2
}
