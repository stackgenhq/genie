// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package jsontool

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the JSON tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the JSON tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the JSON tool.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	j := newJSONTools()
	return []tool.Tool{j.jsonTool()}
}
