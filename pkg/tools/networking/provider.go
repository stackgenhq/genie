// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package networking

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider satisfies the tools.ToolProviders interface for the
// HTTP networking tool. Without this, the networking tool would need
// to be constructed inline inside the registry.
type ToolProvider struct {
	cfg []Config
}

// NewToolProvider creates a ToolProvider for the HTTP request tool.
func NewToolProvider(cfg ...Config) *ToolProvider {
	return &ToolProvider{cfg: cfg}
}

// GetTools returns the HTTP networking tool.
func (p *ToolProvider) GetTools() []tool.Tool {
	return []tool.Tool{NewTool(p.cfg...)}
}
