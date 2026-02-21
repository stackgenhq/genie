// Package tools provides a centralized tool registry that bootstraps all
// tools from various integration points (file, shell, browser, MCP, etc.)
// and filters denied tools based on HITL configuration. This is the single
// source of truth for which tools are available to the orchestrator and
// sub-agents.
package tools

import (
	"context"
	"time"

	"github.com/appcd-dev/genie/pkg/agentutils"
	"github.com/appcd-dev/genie/pkg/browser"
	"github.com/appcd-dev/genie/pkg/clarify"
	"github.com/appcd-dev/genie/pkg/cron"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/runbook"
	"github.com/appcd-dev/genie/pkg/skills"

	"github.com/appcd-dev/genie/pkg/tools/email"
	"github.com/appcd-dev/genie/pkg/tools/networking"
	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/scm"
	"github.com/appcd-dev/genie/pkg/tools/websearch"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	ctxtools "trpc.group/trpc-go/trpc-agent-go/tool/context"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
)

// Deps holds all integration dependencies needed to bootstrap tools.
// Each field is optional — when nil/zero, the corresponding tools are
// simply not registered.
type Deps struct {
	WorkingDir  string
	VectorStore vector.IStore

	// Config sections
	WebSearch   websearch.Config
	SCMConfig   scm.Config
	PMConfig    pm.Config
	EmailConfig email.Config
	SkillsRoots []string
	HITLCfg     hitl.Config

	// Runtime deps (may be nil)
	Messenger messenger.Messenger
	CronStore cron.ICronStore
	MCPClient *mcp.Client      // caller creates & owns lifecycle
	Browser   *browser.Browser // caller creates & owns lifecycle

	// Summarizer for sub-agent output condensation
	Summarizer agentutils.Summarizer

	// ClarifyEmitter bridges clarify tool → AG-UI/messenger notifications
	ClarifyStore   clarify.Store
	ClarifyEmitter clarify.EventEmitter

	// EnablePensieve activates Pensieve context management tools
	// (delete_context, check_budget, note, read_notes).
	EnablePensieve bool
}

// Registry holds the bootstrapped, filtered set of tools.
type Registry struct {
	tools []tool.Tool
	cfg   hitl.Config
}

// NewRegistry creates all tools from the provided dependencies and filters
// out any tools denied by the HITL config. The returned Registry provides
// the filtered tool list via AllTools().
func NewRegistry(ctx context.Context, deps Deps) *Registry {
	log := logger.GetLogger(ctx).With("fn", "toolreg.NewRegistry")

	var tools []tool.Tool

	// --- Always-available tools ---
	tools = append(tools, websearch.NewTool(deps.WebSearch))
	log.Debug("WebSearch tool initialized")

	tools = append(tools, networking.NewTool())
	log.Debug("HTTP request tool initialized")

	// --- MCP tools ---
	if deps.MCPClient != nil {
		tools = append(tools, deps.MCPClient.GetTools()...)
		log.Info("MCP tools added", "count", len(deps.MCPClient.GetTools()))
	}

	// --- Skills ---
	if len(deps.SkillsRoots) != 0 {
		repo, err := skill.NewFSRepository(deps.SkillsRoots...)
		if err != nil {
			log.Warn("failed to initialize skills repository", "error", err)
		}
		if repo != nil {
			executor := skills.NewLocalExecutor(deps.WorkingDir)
			skillTools := skills.CreateAllSkillTools(repo, executor)
			tools = append(tools, skillTools...)
			log.Info("Skills initialized", "tool_count", len(skillTools))
		}
	}

	// --- Browser tools ---
	if deps.Browser != nil {
		for _, bt := range browser.AllTools(deps.Browser) {
			tools = append(tools, bt)
		}
		log.Info("Browser tools added")
	}

	// --- SCM tools ---
	if scmSvc, err := scm.New(deps.SCMConfig); err == nil {
		if vErr := scmSvc.Validate(ctx); vErr != nil {
			log.Warn("SCM health check failed", "error", vErr)
		}
		tools = append(tools, scm.AllTools(scmSvc)...)
	}
	log.Debug("SCM tools initialized")

	// --- PM tools ---
	if pmSvc, err := pm.New(deps.PMConfig); err == nil {
		if vErr := pmSvc.Validate(ctx); vErr != nil {
			log.Warn("PM health check failed", "error", vErr)
		}
		tools = append(tools, pm.AllTools(pmSvc)...)
	}
	log.Debug("PM tools initialized")

	// --- Email tools ---
	if emailSvc, err := deps.EmailConfig.New(); err == nil {
		tools = append(tools, email.AllTools(emailSvc)...)
	}
	log.Debug("Email tools initialized")

	// --- Clarify tool ---
	if deps.ClarifyStore != nil && deps.ClarifyEmitter != nil {
		tools = append(tools, clarify.NewTool(deps.ClarifyStore, deps.ClarifyEmitter))
		log.Debug("Clarify tool initialized")
	}

	// --- Messenger tool ---
	if deps.Messenger != nil {
		tools = append(tools, messenger.NewSendMessageTool(deps.Messenger))
		log.Debug("Messenger send_message tool initialized")
	}

	// --- Cron tool ---
	if deps.CronStore != nil {
		tools = append(tools, cron.NewCreateRecurringTaskTool(deps.CronStore))
		log.Debug("Cron tool initialized")
	}

	// --- File tools (scoped to working directory) ---
	if ts, err := file.NewToolSet(file.WithBaseDir(deps.WorkingDir)); err == nil {
		tools = append(tools, ts.Tools(ctx)...)
		log.Debug("File tools initialized")
	}

	// --- Shell tool ---
	exec := local.New(
		local.WithWorkDir(deps.WorkingDir),
		local.WithTimeout(10*time.Minute),
		local.WithCleanTempFiles(true),
	)
	tools = append(tools, NewShellTool(exec))
	log.Debug("Shell tool initialized")

	// --- Summarizer tool ---
	if deps.Summarizer != nil {
		tools = append(tools, agentutils.NewSummarizerTool(deps.Summarizer))
		log.Debug("Summarizer tool initialized")
	}

	// --- Vector memory + Runbook tools ---
	if deps.VectorStore != nil {
		tools = append(tools,
			vector.NewMemoryStoreTool(deps.VectorStore),
			vector.NewMemorySearchTool(deps.VectorStore),
			runbook.NewSearchTool(deps.VectorStore),
		)
		log.Debug("Vector memory and runbook tools initialized")
	}

	// --- Context management tools (Pensieve paradigm) ---
	// Optional — gated behind EnablePensieve config flag.
	// Enables the LLM to actively manage its own context window:
	//   delete_context: prune processed events (HITL required)
	//   check_budget:   monitor context usage  (auto-approved)
	//   note:           persistent scratchpad  (HITL required)
	//   read_notes:     recall distilled info  (auto-approved)
	if deps.EnablePensieve {
		tools = append(tools, ctxtools.Tools()...)
		log.Debug("Context management tools initialized (Pensieve paradigm)")
	}

	// --- Filter denied tools ---
	var filtered []tool.Tool
	for _, t := range tools {
		name := t.Declaration().Name
		if deps.HITLCfg.IsDenied(name) {
			log.Info("tool denied by config, excluding from registry", "tool", name)
			continue
		}
		filtered = append(filtered, t)
	}

	log.Info("Tool registry initialized", "total", len(tools), "filtered", len(tools)-len(filtered), "available", len(filtered))

	return &Registry{
		tools: filtered,
		cfg:   deps.HITLCfg,
	}
}

// AllTools returns the full set of available (non-denied) tools.
// These are typically used by sub-agents via create_agent.
func (r *Registry) AllTools() []tool.Tool {
	return r.tools
}

// ToolNames returns the names of all available tools (for logging).
func (r *Registry) ToolNames() []string {
	names := make([]string, len(r.tools))
	for i, t := range r.tools {
		names[i] = t.Declaration().Name
	}
	return names
}
