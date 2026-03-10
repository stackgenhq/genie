// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ocrtool

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps the OCR tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct{}

// NewToolProvider creates a ToolProvider for the OCR tool.
func NewToolProvider() *ToolProvider { return &ToolProvider{} }

// GetTools returns the OCR tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	if err := canBootstrap(); err != nil {
		return nil
	}
	return []tool.Tool{
		newOCRTools().ocrTool(),
	}
}
