/*
Copyright © 2026 StackGen, Inc.
*/

// Package doctor runs diagnostic checks on Genie configuration and environment
// (config file, secrets, MCP, SCM, model provider) and returns a list of
// results (errors, warnings, info) with stable ErrCodes for troubleshooting.
//
// It solves the problem of validating setup before or during runs: "genie doctor"
// and the setup wizard use it to report missing keys, invalid URLs, and
// connectivity issues so users can fix configuration without guessing.
package doctor

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/scm"
)

// Severity indicates how severe a check result is.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Result is a single diagnostic from a doctor check.
// Every problem MUST have an ErrCode for troubleshooting docs.
type Result struct {
	ErrCode ErrCode
	Level   Severity
	Section string // e.g. "config", "mcp", "scm", "secrets", "model_config"
	Message string
	Detail  string
}

// Run runs all doctor checks against the loaded config and secret provider.
// cfgPath is the config file path used for loading (may be empty).
// It returns a slice of results; callers should report errors for any Result with Level == SeverityError.
func Run(ctx context.Context, cfg config.GenieConfig, cfgPath string, sp security.SecretProvider) []Result {
	var out []Result
	logr := logger.GetLogger(ctx).With("fn", "doctor.Run")

	// ─── Config ─────────────────────────────────────────────────────────
	if cfgPath == "" {
		out = append(out, Result{
			ErrCode: ErrCodeConfigFileMissing,
			Level:   SeverityInfo,
			Section: "config",
			Message: "No config file found",
			Detail:  "Using defaults. Create .genie.toml or .genie.yaml in the repo or $HOME for full configuration.",
		})
	}

	// ─── Secrets ([security.secrets]) ───────────────────────────────────
	if len(cfg.Security.Secrets) > 0 {
		for name, url := range cfg.Security.Secrets {
			_, err := sp.GetSecret(ctx, name)
			if err != nil {
				out = append(out, Result{
					ErrCode: ErrCodeSecretResolveFailed,
					Level:   SeverityError,
					Section: "secrets",
					Message: fmt.Sprintf("Secret %q could not be resolved", name),
					Detail:  fmt.Sprintf("URL or env: %s — %v", url, err),
				})
			}
		}
	}

	// ─── Model config ───────────────────────────────────────────────────
	if err := cfg.ModelConfig.ValidateAndFilter(ctx, sp, modelprovider.SkipEchoCheck()); err != nil {
		code := ErrCodeModelValidateError
		if strings.Contains(err.Error(), "no valid model providers") {
			code = ErrCodeModelNoProviders
		}
		out = append(out, Result{
			ErrCode: code,
			Level:   SeverityError,
			Section: "model_config",
			Message: "Model provider validation failed",
			Detail:  err.Error(),
		})
	}

	// ─── MCP ────────────────────────────────────────────────────────────
	if err := cfg.MCP.Validate(); err != nil {
		out = append(out, Result{
			ErrCode: ErrCodeMCPConfigInvalid,
			Level:   SeverityError,
			Section: "mcp",
			Message: "MCP configuration invalid",
			Detail:  err.Error(),
		})
	} else if len(cfg.MCP.Servers) > 0 {
		for i := range cfg.MCP.Servers {
			cfg.MCP.Servers[i].SetDefaults()
			srv := cfg.MCP.Servers[i]
			one := mcp.MCPConfig{Servers: []mcp.MCPServerConfig{srv}}
			client, err := mcp.NewClient(ctx, one, mcp.WithSecretProvider(sp))
			if err != nil {
				out = append(out, Result{
					ErrCode: ErrCodeMCPConnectFailed,
					Level:   SeverityError,
					Section: "mcp",
					Message: fmt.Sprintf("MCP server %q: connection failed", srv.Name),
					Detail:  err.Error(),
				})
				continue
			}
			tools := client.GetTools()
			client.Close(ctx)
			logr.Debug("MCP server OK", "server", srv.Name, "tools", len(tools))
		}
	}

	// ─── SCM ────────────────────────────────────────────────────────────
	if cfg.SCM.Provider == "" {
		// Not configured — skip; no ErrCode for "not configured"
	} else {
		if cfg.SCM.Token == "" {
			out = append(out, Result{
				ErrCode: ErrCodeSCMTokenMissing,
				Level:   SeverityError,
				Section: "scm",
				Message: "SCM token is empty",
				Detail:  fmt.Sprintf("Provider %q requires a token. Set token in [scm] or use ${SCM_TOKEN}.", cfg.SCM.Provider),
			})
		} else {
			svc, err := scm.New(cfg.SCM)
			if err != nil {
				out = append(out, Result{
					ErrCode: ErrCodeSCMInitFailed,
					Level:   SeverityError,
					Section: "scm",
					Message: "SCM client failed to initialize",
					Detail:  err.Error(),
				})
			} else {
				if err := svc.Validate(ctx); err != nil {
					out = append(out, Result{
						ErrCode: ErrCodeSCMValidateFailed,
						Level:   SeverityError,
						Section: "scm",
						Message: "SCM connection validation failed",
						Detail:  err.Error(),
					})
				}
			}
		}
	}

	// ─── Messenger (optional) ────────────────────────────────────────────
	if cfg.Messenger.Platform != "" {
		if err := cfg.Messenger.Validate(); err != nil {
			out = append(out, Result{
				ErrCode: ErrCodeMessengerConfigInvalid,
				Level:   SeverityError,
				Section: "messenger",
				Message: "Messenger configuration invalid",
				Detail:  err.Error(),
			})
		}
	}

	return out
}

// HasErrors returns true if any result has Level == SeverityError.
func HasErrors(results []Result) bool {
	return ErrorCount(results) > 0
}

// ErrorCount returns the number of results with Level == SeverityError.
func ErrorCount(results []Result) int {
	var n int
	for _, r := range results {
		if r.Level == SeverityError {
			n++
		}
	}
	return n
}
