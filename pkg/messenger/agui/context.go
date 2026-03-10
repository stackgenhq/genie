// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui

import (
	"context"

	"github.com/stackgenhq/genie/pkg/messenger"
)

// attachmentsKey is the context key for passing media attachments from the
// AG-UI server (handleRun → serverExpert.Handle) to the chatFunc (buildChatHandler)
// without changing the chatFunc signature.
type attachmentsKey struct{}

// WithAttachments returns a new context that carries the given attachments.
func WithAttachments(ctx context.Context, atts []messenger.Attachment) context.Context {
	return context.WithValue(ctx, attachmentsKey{}, atts)
}

// AttachmentsFromContext returns any attachments stored in the context.
// Returns nil if none are present.
func AttachmentsFromContext(ctx context.Context) []messenger.Attachment {
	if v, ok := ctx.Value(attachmentsKey{}).([]messenger.Attachment); ok {
		return v
	}
	return nil
}
