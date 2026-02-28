/*
Copyright © 2026 StackGen, Inc.
*/
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/stackgenhq/genie/pkg/app"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/osutils"

	// Register messenger adapter factories via init().
	_ "github.com/stackgenhq/genie/pkg/messenger/googlechat"
	_ "github.com/stackgenhq/genie/pkg/messenger/slack"
	_ "github.com/stackgenhq/genie/pkg/messenger/teams"
	_ "github.com/stackgenhq/genie/pkg/messenger/telegram"
	_ "github.com/stackgenhq/genie/pkg/messenger/whatsapp"
)

type grantCmd struct {
	rootOpts *rootCmdOption
}

func NewGrantCommand(rootOpts *rootCmdOption) *grantCmd {
	return &grantCmd{
		rootOpts: rootOpts,
	}
}

func (g *grantCmd) command() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Automation wizard",
		Long: `Grant your wish! This command launches an interactive wizard
that helps you describe your goals and executes them using Stackgen's
agentic intelligence.

Examples:
  genie grant`,
		RunE: g.run,
	}
	wd, err := osutils.Getwd()
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}
	cwd, err := filepath.Abs(wd)
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}

	// Bind flags to the grant command options
	cmd.Flags().StringVar(&g.rootOpts.workingDir, "working-dir", cwd, "working directory")

	return cmd, nil
}

// run executes the grant command logic — creates the Application, bootstraps
// all dependencies, and starts the AG-UI HTTP/SSE server.
func (g *grantCmd) run(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	genieCfg, err := g.rootOpts.genieCfg(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	genieCfg.Langfuse.Init(ctx)

	log := logger.GetLogger(ctx).With("fn", "grantCmd.run")
	log.Info("Starting grant command", "version", config.Version)
	application, err := app.NewApplication(genieCfg, g.rootOpts.workingDir)
	if err != nil {
		return fmt.Errorf("app init: %w", err)
	}
	defer application.Close(ctx)

	if err := application.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	return application.Start(ctx)
}
