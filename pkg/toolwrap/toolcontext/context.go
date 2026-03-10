// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolcontext

import "context"

type justificationKey struct{}

func WithJustification(ctx context.Context, justification string) context.Context {
	return context.WithValue(ctx, justificationKey{}, justification)
}

func GetJustification(ctx context.Context) string {
	justification, ok := ctx.Value(justificationKey{}).(string)
	if !ok {
		return ""
	}
	return justification
}

type skipSummarizeSetterKey struct{}

// WithSkipSummarizeSetter injects a setter function into the context.
// Used internally by the summarizer middleware to provide a skip hook.
func WithSkipSummarizeSetter(ctx context.Context, setter func()) context.Context {
	return context.WithValue(ctx, skipSummarizeSetterKey{}, setter)
}

// GetSkipSummarizeSetter retrieves the setter function from the context so
// that external tools (e.g. from agentutils) can invoke it to bypass summarization.
func GetSkipSummarizeSetter(ctx context.Context) func() {
	if setter, ok := ctx.Value(skipSummarizeSetterKey{}).(func()); ok {
		return setter
	}
	return func() {}
}
