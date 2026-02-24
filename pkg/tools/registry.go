// Package tools provides a centralized tool registry that bootstraps all
// tools from various integration points (file, shell, browser, MCP, etc.)
// and filters denied tools based on HITL configuration. This is the single
// source of truth for which tools are available to the orchestrator and
// sub-agents.
package tools

import (
	"context"
	"errors"

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

// Registry holds the bootstrapped, filtered set of tools.
type Registry struct {
	tools map[string]tool.Tool
	cfg   hitl.Config
}

// NewRegistry collects tools from every supplied ToolProviders conformer.
// The returned Registry provides the aggregated tool list via AllTools().
// Use FilterDenied to subsequently exclude tools blocked by HITL config.
func NewRegistry(ctx context.Context, providers ...ToolProviders) *Registry {
	log := logger.GetLogger(ctx).With("fn", "toolreg.NewRegistry")

	tools := make(map[string]tool.Tool)
	for _, provider := range providers {
		for _, t := range provider.GetTools() {
			tools[t.Declaration().Name] = t
		}
	}

	log.Info("Tool registry initialized", "total", len(tools))

	return &Registry{
		tools: tools,
	}
}

func (r *Registry) GetTool(name string) (tool.Tool, error) {
	if t, ok := r.tools[name]; ok {
		return t, nil
	}
	return nil, errors.New("tool not found")
}

// FilterDenied returns a new Registry that excludes any tools denied by
// the HITL config. This is the single place where tool-level deny-listing
// is applied. Without this, denied tools would still be available to agents.
func (r *Registry) FilterDenied(ctx context.Context, cfg hitl.Config) *Registry {
	log := logger.GetLogger(ctx).With("fn", "toolreg.FilterDenied")

	filtered := make(map[string]tool.Tool)
	for name, t := range r.tools {
		if cfg.IsDenied(name) {
			log.Info("tool denied by config, excluding from registry", "tool", name)
			continue
		}
		filtered[name] = t
	}

	log.Info("Tool registry filtered",
		"before", len(r.tools),
		"after", len(filtered),
		"excluded", len(r.tools)-len(filtered))

	return &Registry{
		tools: filtered,
		cfg:   cfg,
	}
}

// GetTools returns the full set of available (non-denied) tools.
// Satisfies the ToolProviders interface so a Registry can be passed
// as a provider to another Registry (e.g. codeOwner tools).
func (r *Registry) GetTools() []tool.Tool {
	var tools []tool.Tool
	for _, t := range r.tools {
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
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Exclude returns a new Registry that omits tools with the given names.
// Used to strip orchestration-only tools (e.g. create_agent, send_message)
// before passing the registry to sub-agents.
func (r *Registry) Exclude(names ...string) *Registry {
	if len(names) == 0 {
		return r
	}
	excludeSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		excludeSet[n] = struct{}{}
	}
	filtered := make(map[string]tool.Tool)
	for name, t := range r.tools {
		if _, skip := excludeSet[name]; !skip {
			filtered[name] = t
		}
	}
	return &Registry{tools: filtered, cfg: r.cfg}
}

// Include returns a new Registry containing only tools whose names appear
// in the provided list. Unknown names are silently ignored. If names is
// empty, the original Registry is returned unchanged (all tools available).
// Used to scope sub-agents to exactly the tools the planner selected.
func (r *Registry) Include(names ...string) *Registry {
	if len(names) == 0 {
		return r
	}
	filtered := make(map[string]tool.Tool, len(names))
	for _, n := range names {
		if t, ok := r.tools[n]; ok {
			filtered[n] = t
		}
	}
	return &Registry{tools: filtered, cfg: r.cfg}
}
