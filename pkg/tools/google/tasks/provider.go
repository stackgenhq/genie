// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps a Tasks Service and satisfies the tools.ToolProviders
// interface so Tasks tools can be passed to tools.NewRegistry. Tools are
// prefixed with the given name (e.g. google_tasks).
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an initialized Tasks service.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all Tasks tools with the given name prefix.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	return AllTools(name, p.svc)
}
