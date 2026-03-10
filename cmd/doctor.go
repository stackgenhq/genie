// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/doctor"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
)

type doctorCmd struct {
	rootOpts *rootCmdOption
}

func newDoctorCommand(rootOpts *rootCmdOption) *cobra.Command {
	c := doctorCmd{rootOpts: rootOpts}
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate configuration, MCP, SCM, and external secrets",
		Long: `Run health checks against your Genie config and report problems.

Loads config from .genie.toml / .genie.yaml (or --config), then checks:
  - Config file and parse
  - External secrets ([security.secrets]) resolution
  - Model provider configuration and tokens
  - MCP server connectivity (each server)
  - SCM (GitHub/GitLab/Bitbucket) connection and token
  - Messenger configuration when present

Every problem includes an ErrCode (e.g. GENIE_DOC_031). Look up codes in
the documentation for troubleshooting steps.`,
		RunE: c.run,
	}
}

func (c *doctorCmd) run(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	logr := logger.GetLogger(ctx).With("fn", "doctor")
	cfgPath := c.rootOpts.cfgFilePath()

	// Load config without requiring model validation so doctor can report all issues.
	envCfg, err := config.LoadGenieConfig(ctx, security.NewEnvProvider(), cfgPath)
	if err != nil {
		fmt.Printf("  ✗ [GENIE_DOC_002] config: %v\n", err)
		fmt.Println("\nSee error codes in the docs (Troubleshooting / Error codes).")
		return err
	}

	var cfg config.GenieConfig
	sp := security.NewEnvProvider()
	if len(envCfg.Security.Secrets) > 0 {
		mgr := security.NewManager(ctx, envCfg.Security)
		defer func() {
			if cerr := mgr.Close(); cerr != nil {
				logr.Warn("failed to close secret manager", "error", cerr)
			}
		}()
		cfg, err = config.LoadGenieConfig(ctx, mgr, cfgPath)
		if err != nil {
			fmt.Printf("  ✗ [GENIE_DOC_002] config: %v\n", err)
			return err
		}
		sp = mgr
	} else {
		cfg = envCfg
	}

	results := doctor.Run(ctx, cfg, cfgPath, sp)

	if len(results) == 0 {
		fmt.Println("All checks passed.")
		return nil
	}

	for _, r := range results {
		var sym string
		switch r.Level {
		case doctor.SeverityError:
			sym = "✗"
		case doctor.SeverityWarning:
			sym = "⚠"
		default:
			sym = "✓"
		}
		fmt.Printf("  %s [%s] %s: %s\n", sym, r.ErrCode, r.Section, r.Message)
		if r.Detail != "" {
			fmt.Printf("      %s\n", r.Detail)
		}
	}

	fmt.Println("\nError codes: see docs (Troubleshooting / Error codes) for resolution steps.")

	if doctor.HasErrors(results) {
		n := doctor.ErrorCount(results)
		return fmt.Errorf("doctor found %d error(s)", n)
	}
	return nil
}
