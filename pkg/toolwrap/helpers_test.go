// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"sync/atomic"

	"github.com/stackgenhq/genie/pkg/toolwrap"
)

// passthrough is a Handler that always succeeds with a fixed value.
func passthrough(result any) toolwrap.Handler {
	return func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
		return result, nil
	}
}

// failing is a Handler that always returns an error.
func failing(err error) toolwrap.Handler {
	return func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
		return nil, err
	}
}

// counting wraps a handler and counts how many times it's invoked.
func counting(h toolwrap.Handler) (toolwrap.Handler, *int32) {
	var n int32
	return func(ctx context.Context, tc *toolwrap.ToolCallContext) (any, error) {
		atomic.AddInt32(&n, 1)
		return h(ctx, tc)
	}, &n
}

func tc(name string) *toolwrap.ToolCallContext {
	return &toolwrap.ToolCallContext{ToolName: name, Args: []byte(`{}`)}
}
