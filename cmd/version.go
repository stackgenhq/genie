package cmd

import (
	"fmt"

	"github.com/appcd-dev/go-lib/constants"
	"github.com/spf13/cobra"
)

type versionCmd struct {
	rootOpts *rootCmdOption
}

func newVersionCommand(rootOpts *rootCmdOption) *cobra.Command {
	v := versionCmd{rootOpts: rootOpts}
	return v.command()
}

func (v versionCmd) command() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the version of Genie",
		Long:  `Tells how old the Genie is`,
		RunE:  v.run,
	}
}

func (v versionCmd) run(cmd *cobra.Command, args []string) error {
	fmt.Println(constants.Version)
	return nil
}
