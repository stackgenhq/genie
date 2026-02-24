package sqltool

import (
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the SQL query tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct {
	secretProvider security.SecretProvider
}

// NewToolProvider creates a ToolProvider for the SQL query tool.
func NewToolProvider(secretProvider security.SecretProvider) *ToolProvider {
	return &ToolProvider{secretProvider: secretProvider}
}

// GetTools returns the SQL query tool.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	s := NewSQLTools(name, p.secretProvider)
	return []tool.Tool{s.queryTool()}
}
