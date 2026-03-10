// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import "strings"

// errorPatterns are phrases that indicate an LLM response is an error message
// rather than a genuine result. Used to prevent poisoning episodic memory
// with unhelpful error text that would degrade future responses.
var errorPatterns = []string{
	"an error occurred",
	"error occurred during execution",
	"please contact the service provider",
	"failed to process",
	"internal server error",
	"i encountered an error",
	"something went wrong",
}

// looksLikeError returns true if the output contains known error patterns.
// These are canned error responses from LLM providers or the agent framework
// that should not be stored as successful episodes in episodic memory.
func looksLikeError(output string) bool {
	if output == "" {
		return true // empty output is not useful to store
	}
	lower := strings.ToLower(output)
	for _, pattern := range errorPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
