// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package math

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the mathematical tools and satisfies the
// tools.ToolProviders interface so it can be passed directly to
// tools.NewRegistry. Without this, math tools would need to be
// constructed inline inside the registry.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the mathematical tools.
func NewToolProvider() *ToolProvider {
	return &ToolProvider{}
}

// GetTools returns the mathematical tools: a unified arithmetic tool (math)
// and an expression evaluator (calculator).
func (p *ToolProvider) GetTools() []tool.Tool {
	m := newMathTools()
	return []tool.Tool{
		m.mathTool(),
		m.calculatorTool(),
	}
}
