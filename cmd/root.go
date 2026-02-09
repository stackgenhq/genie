/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
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
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/go-lib/constants"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/spf13/cobra"
)

type rootCmdOption struct {
	cfgFile  string
	logLevel string
	codeDir  string
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
		return slog.LevelWarn
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

	cwd, err := filepath.Abs(osutils.Getwd())
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}
	// rootCmd represents the base command when called without any subcommands
	rootCmd := &cobra.Command{
		Use:   "genie",
		Short: "The world's first Agentic IaC CLI",
		Long: `🧞 genie - Your intent is my command!

genie is the world's first Agentic IaC CLI, powered by Stackgen. We're moving 
beyond the era of manual configuration and into the era of Intent-to-Infrastructure.

Stop writing YAML. Stop debugging Terraform modules. Just tell genie what you need, 
and consider it granted.

Built with ✨ by Stackgen (https://stackgen.com)
Infrastructure is hard. Being a Genie is easy.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			err := langfuse.StartTrace(cmd.Context())
			if err != nil {
				logger.GetLogger(cmd.Context()).Error("Failed to start trace", "error", err)
				return nil
			}
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
	rootCmd.AddCommand(newMCPCommand(&r.opts))
	rootCmd.AddCommand(newVersionCommand(&r.opts))
	rootCmd.AddCommand(newAnalyzeCommand(&r.opts).command())
	// Global flags
	rootCmd.PersistentFlags().StringVar(&r.opts.cfgFile, "config", "", "config file (default is $HOME/.genie.yaml; repo .genie.yaml preferred)")
	rootCmd.PersistentFlags().StringVar(&r.opts.logLevel, "log-level", "warn", "log level (debug, info, warn, error)")

	// Grant command flags (also available at root level for convenience)
	rootCmd.PersistentFlags().StringVar(&r.opts.codeDir, "code-dir", cwd, "code directory")
	rootCmd.PersistentFlags().StringVar(&grantCmd.opts.SaveTo, "save-to", filepath.Join(cwd, "genie_output"), "save to")

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
		logger.GetLogger(ctx).Debug("genie CLI starting", "version", constants.Version, "config", cfgFile)
	} else {
		logger.GetLogger(ctx).Debug("genie CLI starting", "version", constants.Version)
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
