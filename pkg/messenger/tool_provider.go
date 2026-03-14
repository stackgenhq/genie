// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps a Messenger and satisfies the tools.ToolProviders
// interface so the send_message tool can be passed directly to
// tools.NewRegistry. Without this, messenger tool construction would
// be inlined in the registry.
type ToolProvider struct {
	msgr Messenger
}

// NewToolProvider creates a ToolProvider for the send_message tool.
func NewToolProvider(msgr Messenger) *ToolProvider {
	return &ToolProvider{msgr: msgr}
}

// GetTools returns the send_message tool wired to the underlying messenger.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	return []tool.Tool{NewSendMessageTool(p.msgr)}
}
