// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gmail

import (
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps a Gmail Service and satisfies the tools.ToolProviders
// interface so Gmail tools can be passed to tools.NewRegistry. Tools are
// prefixed with the given name (e.g. google_gmail).
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an initialized Gmail service.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all Gmail tools with the given name prefix.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	return AllTools(name, p.svc)
}
