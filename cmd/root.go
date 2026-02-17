/*
Copyright © 2026 StackGen, Inc.
*/
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/appcd-dev/genie/pkg/config"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/osutils"
	"github.com/spf13/cobra"
)

type rootCmdOption struct {
	cfgFile    string
	logLevel   string
	workingDir string
}

func (r rootCmdOption) level() slog.Level {
	switch strings.ToLower(r.logLevel) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type rootCmd struct {
	opts rootCmdOption
}

func NewRootCommand() rootCmd {
	return rootCmd{}
}

func (r rootCmd) command(ctx context.Context) (*cobra.Command, error) {
	grantCmd := NewGrantCommand(&r.opts)

	wd, err := osutils.Getwd()
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}
	cwd, err := filepath.Abs(wd)
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}
	// rootCmd represents the base command when called without any subcommands
	rootCmd := &cobra.Command{
		Use:   "genie",
		Short: "The Enterprise Agentic Platform",
		Long: `🧞 Genie - Your intent is my command!

genie is the Enterprise Agentic Infrastructure Automation CLI, powered by Stackgen. We're moving 
beyond the era of manual configuration.

Just tell genie what you need, 
and consider it granted.

Built with ✨ by Stackgen (https://stackgen.com)
Infrastructure is hard. Being a Genie is easy.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return r.init(cmd.Context())
		},
		RunE: grantCmd.run,
	}

	// Add subcommands
	grantCobraCmd, err := grantCmd.command()
	if err != nil {
		return nil, err
	}
	rootCmd.AddCommand(grantCobraCmd)
	rootCmd.AddCommand(newVersionCommand(&r.opts))
	rootCmd.AddCommand(newConnectCommand(&r.opts))
	// Global flags
	rootCmd.PersistentFlags().StringVar(&r.opts.cfgFile, "config", "", "config file (default is $HOME/.genie.yaml; repo .genie.yaml preferred)")
	rootCmd.PersistentFlags().StringVar(&r.opts.logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	// Grant command flags (also available at root level for convenience)
	rootCmd.PersistentFlags().StringVar(&r.opts.workingDir, "working-dir", cwd, "code directory")

	return rootCmd, nil
}

func (opts rootCmdOption) genieCfg() (config.GenieConfig, error) {
	genieCfg, err := config.LoadGenieConfig(opts.cfgFilePath())
	if err != nil {
		return config.GenieConfig{}, err
	}
	return genieCfg, nil
}

func (opts rootCmdOption) cfgFilePath() string {
	if opts.cfgFile != "" {
		return opts.cfgFile
	}
	candidates := []string{".genie.yaml", ".genie.yml", "genie.yaml", "genie.yml", ".genie.toml", "genie.toml"}
	if cwd, err := os.Getwd(); err == nil {
		for _, c := range candidates {
			p := filepath.Join(cwd, c)
			if _, err := os.Stat(p); err == nil {
				return filepath.Clean(p)
			}
		}
	}

	// Fallback to $HOME/.genie.yaml if present
	if home, err := os.UserHomeDir(); err == nil {
		for _, c := range candidates {
			p := filepath.Join(home, c)
			if _, err := os.Stat(p); err == nil {
				return filepath.Clean(p)
			}
		}
	}
	return ""
}

func (r rootCmd) init(ctx context.Context) error {
	// Prefer a repo-local config file if no `--config` was provided.

	cfgFile := r.opts.cfgFilePath()

	// Determine log level based on flags (possibly overridden by config)
	logger.SetLogLevel(r.opts.level())
	logger.SendJSONTo(os.Stderr)

	// Log startup info at debug level, include config path if available
	if cfgFile != "" {
		logger.GetLogger(ctx).Debug("genie CLI starting", "version", Version, "config", cfgFile)
	} else {
		logger.GetLogger(ctx).Debug("genie CLI starting", "version", Version)
	}

	return nil
}

func (r rootCmd) ExecuteContext(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cmd, err := r.command(ctx)
	if err != nil {
		return err
	}
	return cmd.ExecuteContext(ctx)
}
