// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agentutils

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// SummarizerToolProvider wraps a Summarizer and satisfies the
// tools.ToolProviders interface so the summarize_content tool can be
// passed directly to tools.NewRegistry.
type SummarizerToolProvider struct {
	summarizer Summarizer
}

// NewSummarizerToolProvider creates a ToolProvider for the summarizer tool.
func NewSummarizerToolProvider(s Summarizer) *SummarizerToolProvider {
	return &SummarizerToolProvider{summarizer: s}
}

// GetTools returns the summarizer tool.
func (p *SummarizerToolProvider) GetTools(_ context.Context) []tool.Tool {
	return []tool.Tool{NewSummarizerTool(p.summarizer)}
}
