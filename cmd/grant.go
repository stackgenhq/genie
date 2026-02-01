/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type grantCmdOption struct {
}

type grantCmd struct {
	opts grantCmdOption
}

func NewGrantCommand() grantCmd {
	return grantCmd{}
}

func (g grantCmd) command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Interactive Intent-to-Infrastructure wizard",
		Long: `Grant your infrastructure wish! This command launches an interactive wizard
that helps you describe your application requirements and generates production-ready
infrastructure code using Stackgen's agentic intelligence.

Examples:
  genie grant`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.run(cmd.Context())
		},
	}

	return cmd
}

func (g grantCmd) run(ctx context.Context) error {

	fmt.Println("🧞 Your wish is my command!")
	fmt.Println()
	return nil
}
