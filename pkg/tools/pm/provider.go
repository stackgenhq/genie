// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pm

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps a PM Service and satisfies the tools.ToolProviders
// interface so project management tools can be passed directly to
// tools.NewRegistry. Without this, PM tool construction would be
// inlined in the registry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised PM service.
// Callers are responsible for creating and validating the service beforehand.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all PM tools wired to the underlying service.
func (p *ToolProvider) GetTools() []tool.Tool {
	return AllTools(p.svc)
}
