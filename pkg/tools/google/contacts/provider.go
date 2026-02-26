package contacts

import (
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the contacts tools and satisfies the tools.ToolProviders
// interface so contacts tools can be passed to tools.NewRegistry. Secrets are
// resolved at runtime (not at construction time). Uses the same embedded OAuth
// client as Calendar when built with -X (see pkg/tools/google/oauth).
type ToolProvider struct {
	secretProvider security.SecretProvider
}

// NewToolProvider creates a ToolProvider for the contacts tools.
func NewToolProvider(secretProvider security.SecretProvider) *ToolProvider {
	return &ToolProvider{secretProvider: secretProvider}
}

// GetTools returns all contacts tools, prefixed with the given name.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	c := newContactsTools(name, p.secretProvider)
	callables := c.tools()
	tools := make([]tool.Tool, len(callables))
	for i, t := range callables {
		tools[i] = t
	}
	return tools
}
