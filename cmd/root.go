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

	"github.com/spf13/cobra"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/osutils"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/keyring"
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

genie is an Enterprise Agentic Platform powered by Stackgen. Describe what you need —
across code, operations, security, and beyond — and let genie plan and execute it.

Built with ✨ by Stackgen (https://stackgen.com)
Complex tasks are hard. Being a Genie is easy.`,
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
	rootCmd.AddCommand(newSetupCommand())
	rootCmd.AddCommand(newVersionCommand(&r.opts))
	rootCmd.AddCommand(newConnectCommand(&r.opts))
	rootCmd.AddCommand(newBackToBottleCommand())
	rootCmd.AddCommand(newDoctorCommand(&r.opts))
	// Global flags
	rootCmd.PersistentFlags().StringVar(&r.opts.cfgFile, "config", "", "config file (default is $HOME/.genie.yaml; repo .genie.yaml preferred)")
	rootCmd.PersistentFlags().StringVar(&r.opts.logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	// Grant command flags (also available at root level for convenience)
	rootCmd.PersistentFlags().StringVar(&r.opts.workingDir, "working-dir", cwd, "code directory")

	return rootCmd, nil
}

func (opts rootCmdOption) genieCfg(ctx context.Context) (config.GenieConfig, error) {
	cfgPath := opts.cfgFilePath()

	// Pass 1: load with env-only provider to extract security config.
	envCfg, err := config.LoadGenieConfig(ctx, security.NewEnvProvider(), cfgPath)
	if err != nil {
		return config.GenieConfig{}, err
	}

	// If no runtimevar secret mappings are configured, the env-only
	// config is final — backward-compatible with existing setups.
	if len(envCfg.Security.Secrets) == 0 {
		if err := envCfg.ModelConfig.ValidateAndFilter(ctx, security.NewEnvProvider()); err != nil {
			return config.GenieConfig{}, err
		}
		envCfg.PII.Apply()
		config.WarnMissingTokens(ctx, envCfg, cfgPath)
		return envCfg, nil
	}

	// Pass 2: create a Manager backed by the [security.secrets] URLs
	// and reload so that ${VAR} placeholders resolve through the
	// runtimevar backends (GCP SM, AWS SM, files, etc.).
	mgr := security.NewManager(ctx, envCfg.Security)
	defer func() {
		if cerr := mgr.Close(); cerr != nil {
			logger.GetLogger(ctx).Warn("failed to close secret manager", "error", cerr)
		}
	}()

	genieCfg, err := config.LoadGenieConfig(ctx, mgr, cfgPath)
	if err != nil {
		return config.GenieConfig{}, err
	}

	if err := genieCfg.ModelConfig.ValidateAndFilter(ctx, mgr); err != nil {
		return config.GenieConfig{}, err
	}

	// Apply PII redaction config to pii-shield's scanner before any
	// memory storage operations use it.
	genieCfg.PII.Apply()

	config.WarnMissingTokens(ctx, genieCfg, cfgPath)

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

	// Scope keyring by config path so multiple Genie instances (different configs)
	// do not overwrite each other's keychain data (e.g. Google OAuth).
	keyring.SetKeyringScopeFromConfigPath(cfgFile)

	// Determine log level based on flags (possibly overridden by config)
	logger.SetLogLevel(r.opts.level())
	logger.SendJSONTo(os.Stderr)

	// Log startup info at debug level, include config path if available
	if cfgFile != "" {
		logger.GetLogger(ctx).Debug("genie CLI starting", "version", config.Version, "config", cfgFile)
	} else {
		logger.GetLogger(ctx).Debug("genie CLI starting", "version", config.Version)
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
