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

type contextKey string

var (
	replacerKey      contextKey = "replacer"
	skipPIIRedaction contextKey = "skip_pii_redaction"
)

// WithReplacer stores the given replacer in ctx. Used by model BeforeModel
// callbacks so tool execution and AfterModel can rehydrate [HIDDEN:hash] back
// to original values (e.g. email addresses in email_send arguments).
func WithReplacer(ctx context.Context, r *strings.Replacer) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, replacerKey, r)
}

// ReplacerFromContext returns the replacer from ctx, or nil. Used by toolwrap
// to rehydrate tool-call arguments before execution, and by AfterModel to
// rehydrate assistant response content.
func ReplacerFromContext(ctx context.Context) *strings.Replacer {
	r, _ := ctx.Value(replacerKey).(*strings.Replacer)
	return r
}

// WithSkipRedactionContext returns a new context with skip_pii_redaction set to true.
func WithSkipRedactionContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipPIIRedaction, true)
}

// SkipRedactionFromContext returns true if skip_pii_redaction is set in ctx.
func SkipRedactionFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(skipPIIRedaction).(bool)
	return v
}

// SkipRedactionMarker is a plain-text marker that tool results can embed to
// signal that PII redaction should be skipped for that message. This is used
// by auth tools where OAuth URLs contain parameters (e.g. client_id) that
// look like secrets to the entropy scanner but are intentionally exposed.
//
// The marker uses only ASCII alphanumeric + underscores so it survives
// JSON encoding (unlike HTML comments whose < > get escaped to \u003c \u003e).
const SkipRedactionMarker = "__SKIP_PII_REDACTION__"

// ContentHasSkipMarker returns true if text contains the SkipRedactionMarker.
func ContentHasSkipMarker(text string) bool {
	return strings.Contains(text, SkipRedactionMarker)
}
