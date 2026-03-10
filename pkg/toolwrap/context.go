// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import "context"

type ctxKey struct{ name string }

var originalQuestionKey = &ctxKey{name: "original_question"}

// WithOriginalQuestion stashes the user's original question in the context
// so that requireApproval can persist it alongside the approval row.
// Without this, recovered approvals after a restart would have no way to
// replay the original request through the chat handler.
func WithOriginalQuestion(ctx context.Context, question string) context.Context {
	return context.WithValue(ctx, originalQuestionKey, question)
}

// OriginalQuestionFrom extracts the original question from the context.
func OriginalQuestionFrom(ctx context.Context) string {
	val, _ := ctx.Value(originalQuestionKey).(string)
	return val
}
