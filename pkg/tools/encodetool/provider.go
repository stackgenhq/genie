// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package encodetool

import (
	"context"

	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolProvider wraps the encode tool and satisfies the tools.ToolProviders interface.
type ToolProvider struct {
	crypto security.CryptoConfig
}

// NewToolProvider creates a ToolProvider for the encode tool. CryptoConfig is passed so the provider
// shares the same security policy as the rest of the app; weak algorithms (e.g. MD5) are always disabled.
func NewToolProvider(crypto security.CryptoConfig) *ToolProvider {
	return &ToolProvider{crypto: crypto}
}

// GetTools returns the encode tool.
func (p *ToolProvider) GetTools(_ context.Context) []tool.Tool {
	e := newEncodeTools()
	return []tool.Tool{e.encodeTool()}
}
