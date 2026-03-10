// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stackgenhq/genie/pkg/security/keyring"
)

// newBackToBottleCommand returns the back-to-bottle cobra command. It performs
// cleanup of Genie keyring data (and can be extended for other local state).
func newBackToBottleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "back-to-bottle",
		Short: "Clean up Genie keyring and local secret data",
		Long: `Back-to-bottle removes Genie secrets from the system keyring (service "genie").
Use this to reset stored API keys, Google OAuth tokens, and other keyring-backed
secrets. Config files are not modified.`,
		RunE: runBackToBottle,
	}
}

func runBackToBottle(cmd *cobra.Command, args []string) error {
	deleted, err := keyring.KeyringDeleteAllGenie()
	if err != nil {
		return fmt.Errorf("keyring cleanup: %w", err)
	}
	if deleted == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "No Genie keyring entries found; nothing to remove.")
		return nil
	}
	if deleted == 1 {
		_, _ = fmt.Fprintln(os.Stdout, "Removed 1 Genie keyring entry.")
		return nil
	}
	_, _ = fmt.Fprintf(os.Stdout, "Removed %d Genie keyring entries.\n", deleted)
	return nil
}
