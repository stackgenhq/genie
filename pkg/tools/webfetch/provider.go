// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package webfetch

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the web fetch tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the web fetch tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the web fetch tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	f := newFetchTools()
	return []tool.Tool{f.fetchTool()}
}
