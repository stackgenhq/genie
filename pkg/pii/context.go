// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package pii — context helpers for PII rehydration.
package pii

import (
	"context"
	"strings"
)

// replacerKey is the context key for the *strings.Replacer built in BeforeModel
// (RedactWithReplacer). It maps [HIDDEN:hash] → original so tool args and
// response text can be rehydrated before execution or display.
type replacerKey struct{}

var replacerKeyVar = &replacerKey{}

// WithReplacer stores the given replacer in ctx. Used by model BeforeModel
// callbacks so tool execution and AfterModel can rehydrate [HIDDEN:hash] back
// to original values (e.g. email addresses in email_send arguments).
func WithReplacer(ctx context.Context, r *strings.Replacer) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, replacerKeyVar, r)
}

// ReplacerFromContext returns the replacer from ctx, or nil. Used by toolwrap
// to rehydrate tool-call arguments before execution, and by AfterModel to
// rehydrate assistant response content.
func ReplacerFromContext(ctx context.Context) *strings.Replacer {
	r, _ := ctx.Value(replacerKeyVar).(*strings.Replacer)
	return r
}
