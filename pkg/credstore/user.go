// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"

	"github.com/stackgenhq/genie/pkg/messenger"
)

// defaultUserID is used when no MessageOrigin is in context (e.g. local
// single-user Genie, or system-initiated calls).
const defaultUserID = "_default"

// userIDFromContext extracts the current user's ID from ctx via
// MessageOrigin.Sender.ID. Returns defaultUserID when the origin is
// absent or zero (backward-compatible with single-user local mode).
func userIDFromContext(ctx context.Context) string {
	origin := messenger.MessageOriginFrom(ctx)
	if origin.IsZero() || origin.Sender.ID == "" {
		return defaultUserID
	}
	return origin.Sender.ID
}
