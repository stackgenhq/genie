// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package calendar: OAuth client is shared with other Google tools (Contacts).
// See pkg/tools/google/oauth for build-time injection of GoogleClientID and
// GoogleClientSecret. Calendar uses the same client so one OAuth consent can
// grant Calendar + Contacts.

package calendar

import (
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
)

// getCredentialsForCalendar returns either the user-provided credentials JSON
// (from credsEntry) or the shared embedded build-time credentials.
func getCredentialsForCalendar(credsEntry string) ([]byte, error) {
	return oauth.GetCredentials(credsEntry, "Calendar")
}
