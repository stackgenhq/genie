// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package osutils

import "os"

// Getenv returns the value of the environment variable named by key.
// If the variable is not set, defaultValue is returned.
// Without this function, callers would need to manually check for empty
// env vars and provide fallback values throughout the codebase.
func Getenv(key string, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
