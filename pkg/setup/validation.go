// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package setup

import (
	"fmt"
	"path/filepath"
)

// ValidateConfigPath returns an error if s is empty or not a valid path.
// Used when the user passes --config to genie setup.
func ValidateConfigPath(s string) error {
	if s == "" {
		return fmt.Errorf("please enter a path")
	}
	_, err := filepath.Abs(s)
	if err != nil {
		return fmt.Errorf("that path isn't valid: %w", err)
	}
	return nil
}
