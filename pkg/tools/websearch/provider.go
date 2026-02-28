package websearch

import (
	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps websearch configuration and satisfies the
// tools.ToolProviders interface so it can be passed directly to
// tools.NewRegistry. Without this, websearch tools would need to be
// constructed inline inside the registry.
type ToolProvider struct {
	cfg  Config
	sp   security.SecretProvider // optional; when set, Google provider can use shared OAuth token
	opts []DDGOption
}

// ProviderOption configures the websearch ToolProvider (e.g. WithSecretProvider).
type ProviderOption func(*ToolProvider)

// WithSecretProvider sets the SecretProvider so Google search can use the shared
// Google OAuth token (same sign-in as Calendar, Drive, Gmail) when UseGoogleOAuth
// is true or google_api_key is not set.
func WithSecretProvider(sp security.SecretProvider) ProviderOption {
	return func(p *ToolProvider) { p.sp = sp }
}

// WithDDGOptions passes options to the fallback DuckDuckGo tool (e.g. for testing).
func WithDDGOptions(ddgOpts ...DDGOption) ProviderOption {
	return func(p *ToolProvider) { p.opts = ddgOpts }
}

// NewToolProvider creates a ToolProvider from the given websearch config.
// Pass WithSecretProvider(sp) to enable Google search via the shared OAuth token.
func NewToolProvider(cfg Config, opts ...ProviderOption) *ToolProvider {
	p := &ToolProvider{cfg: cfg}
	for _, o := range opts {
		o(p)
	}
	return p
}

// GetTools returns the websearch tool configured from the provider's config,
// as well as the separate wikipedia_search tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{
		NewTool(p.cfg, p.sp, p.opts...),
		NewWikipediaTool(),
	}
}
