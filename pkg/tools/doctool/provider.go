// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package doctool

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the document parsing tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the document parsing tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the document parsing tool.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	d := newDocTools()
	return []tool.Tool{d.docParseTool()}
}
