// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package osutils

import "strings"

// SanitizeForFilename returns a filesystem-safe version of the given name:
// lowercase, alphanumeric and underscore only; spaces and hyphens become underscore.
// Other characters are dropped. Empty input yields an empty string.
// Used for agent names, report names, and similar identifiers in paths
// (e.g. ~/.genie/reports/<agent>/<date>_<report>.md). Without this function,
// user-supplied names could produce invalid or unsafe filenames.
func SanitizeForFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' {
			b.WriteRune('_')
		}
	}
	return b.String()
}
