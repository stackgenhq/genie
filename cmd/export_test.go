package cmd

import "github.com/spf13/cobra"

// RootCmdOption is exported for testing purposes only.
type RootCmdOption = rootCmdOption

// NewConnectCommand creates a connect cobra command (exported for tests).
func NewConnectCommand(rootOpts *RootCmdOption) *cobra.Command {
	return newConnectCommand(rootOpts)
}

// Command exposes grantCmd.command for testing.
func (g *grantCmd) Command() (*cobra.Command, error) {
	return g.command()
}
