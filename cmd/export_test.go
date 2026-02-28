package cmd

import "github.com/spf13/cobra"

// RootCmdOption is exported for tests only. Not part of the public API.
type RootCmdOption = rootCmdOption

// NewConnectCommand creates a connect cobra command for tests only. Not part of the public API.
func NewConnectCommand(rootOpts *RootCmdOption) *cobra.Command {
	return newConnectCommand(rootOpts)
}

// Command exposes grantCmd.command for tests only. Not part of the public API.
func (g *grantCmd) Command() (*cobra.Command, error) {
	return g.command()
}
