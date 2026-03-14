// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package websearch

import (
	"context"

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
// as well as the separate wikipedia_search tool. When SerpAPI is the selected
// provider, the google_news_search and google_scholar_search tools are also
// included for specialised search workflows.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	tools := []tool.Tool{
		NewTool(p.cfg, p.sp, p.opts...),
		NewWikipediaTool(),
	}

	// When SerpAPI is the active provider, add specialised tools for
	// Google News and Google Scholar — they use the same API key.
	if normaliseProvider(p.cfg.Provider) == ProviderSerpAPI && p.cfg.SerpAPI.APIKey != "" {
		tools = append(tools,
			NewSerpAPINewsTool(p.cfg.SerpAPI),
			NewSerpAPIScholarTool(p.cfg.SerpAPI),
		)
	}

	return tools
}
