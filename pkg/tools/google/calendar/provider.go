// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package calendar

import (
	gcal "google.golang.org/api/calendar/v3"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the calendar tools and satisfies the tools.ToolProviders
// interface so calendar tools can be passed directly to tools.NewRegistry.
// Each calendar instance is identified by a name (e.g. "work", "personal")
// which prefixes all tool names so the LLM can distinguish between multiple
// calendar integrations.
type ToolProvider struct {
	svc *gcal.Service
}

// NewToolProvider creates a ToolProvider for the calendar tools.
// The service is pre-initialized and authenticated.
func NewToolProvider(svc *gcal.Service) *ToolProvider {
	return &ToolProvider{
		svc: svc,
	}
}

// GetTools returns all calendar tools as individual callable tools, prefixed
// with the given name. Each tool has a dedicated typed request struct so the
// LLM schema is precise and unambiguous per operation.
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	c := newCalendarTools(name, p.svc)
	callables := c.tools()
	tools := make([]tool.Tool, len(callables))
	for i, t := range callables {
		tools[i] = t
	}
	return tools
}
