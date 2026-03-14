// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package browser

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// GetTools satisfies the tools.ToolProviders interface so a Browser
// instance can be passed directly to tools.NewRegistry. Without this,
// browser tool construction would be inlined in the registry.
func (b *Browser) GetTools(_ context.Context) []tool.Tool {
	callables := AllTools(b)
	out := make([]tool.Tool, len(callables))
	for i, t := range callables {
		out[i] = t
	}
	return out
}
