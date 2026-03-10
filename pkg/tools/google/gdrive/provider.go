// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gdrive

import "trpc.group/trpc-go/trpc-agent-go/tool"

// ToolProvider wraps a Google Drive Service and satisfies the tools.ToolProviders
// interface so Google Drive tools can be passed directly to tools.NewRegistry.
type ToolProvider struct {
	svc Service
}

// NewToolProvider creates a ToolProvider from an already-initialised Google Drive service.
func NewToolProvider(svc Service) *ToolProvider {
	return &ToolProvider{svc: svc}
}

// GetTools returns all Google Drive tools wired to the underlying service,
// with tool names prefixed by name (e.g. "google_drive" -> google_drive_search).
func (p *ToolProvider) GetTools(name string) []tool.Tool {
	return AllTools(name, p.svc)
}
