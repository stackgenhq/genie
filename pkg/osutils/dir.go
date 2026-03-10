// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package osutils

import (
	"os"
	"path/filepath"
)

// GenieDir returns the path to the Genie directory in the user's home directory.
// It creates the directory if it does not exist.
func GenieDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "genie"
	}
	dir := filepath.Join(home, ".genie")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
