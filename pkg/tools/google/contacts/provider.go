// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package contacts

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the contacts tools and satisfies the tools.ToolProviders
// interface so contacts tools can be passed to tools.NewRegistry.
// Uses the same embedded OAuth client as Calendar when built with -X.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider for the contacts tools.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all contacts tools, prefixed with the given name.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	c := newContactsTools(name, p.svc)
	callables := c.tools()
	tools := make([]tool.Tool, len(callables))
	for i, t := range callables {
		tools[i] = t
	}
	return tools
}
