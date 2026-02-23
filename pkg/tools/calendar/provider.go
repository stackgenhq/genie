package calendar

import (
	"github.com/appcd-dev/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the calendar tools and satisfies the tools.ToolProviders
// interface so calendar tools can be passed directly to tools.NewRegistry.
// Each calendar instance is identified by a name (e.g. "work", "personal")
// which prefixes all tool names so the LLM can distinguish between multiple
// calendar integrations. Secrets are resolved at runtime (not construction
// time) so that rotated credentials are picked up automatically.
type ToolProvider struct {
	secretProvider security.SecretProvider
}

// NewToolProvider creates a ToolProvider for the calendar tools.
// The SecretProvider is stored but secrets are only resolved when
// each tool handler executes, supporting credential rotation.
func NewToolProvider(secretProvider security.SecretProvider) *ToolProvider {
	return &ToolProvider{
		secretProvider: secretProvider,
	}
}

// GetTools returns all calendar tools as individual callable tools, prefixed
// with the given name. Each tool has a dedicated typed request struct so the
// LLM schema is precise and unambiguous per operation.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	c := newCalendarTools(name, p.secretProvider)
	callables := c.tools()
	tools := make([]tool.Tool, len(callables))
	for i, t := range callables {
		tools[i] = t
	}
	return tools
}
