// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scm

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps an SCM Service and satisfies the tools.ToolProviders
// interface so SCM tools can be passed directly to tools.NewRegistry.
// Without this, SCM tool construction would be inlined in the registry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised SCM service.
// Callers are responsible for creating and validating the service beforehand.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all SCM tools wired to the underlying service.
func (p *ToolProvider) GetTools() []tool.Tool {
	return AllTools(p.svc)
}
