package skills

import (
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps a skill repository and executor and satisfies the
// tools.ToolProviders interface so skill tools can be passed directly
// to tools.NewRegistry. Without this, skill tool construction would
// be inlined in the registry.
type ToolProvider struct {
	repo     skill.Repository
	executor Executor
}

// NewToolProvider creates a ToolProvider for skills tools.
// Callers are responsible for creating the repository and executor.
func NewToolProvider(repo skill.Repository, executor Executor) *ToolProvider {
	return &ToolProvider{repo: repo, executor: executor}
}

// GetTools returns all skill tools (load, run, list) wired to the
// underlying repository and executor.
func (p *ToolProvider) GetTools() []tool.Tool {
	return CreateAllSkillTools(p.repo, p.executor)
}
