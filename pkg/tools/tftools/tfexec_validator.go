package tftools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"os"
	"path/filepath"

	"github.com/appcd-dev/go-lib/logger"
	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// tfExecValidator validates Terraform/OpenTofu configurations using terraform/tofu validate
type tfExecValidator struct{}

// tfExecValidatorOutput represents the output of Terraform validation
type tfExecValidatorOutput struct {
	IsValid  bool     `json:"is_valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Validate runs terraform/tofu validate on the given directory
func (v *tfExecValidator) validate(ctx context.Context, req TFValidatorInput) (tfExecValidatorOutput, error) {
	if req.IACPath == "" {
		return tfExecValidatorOutput{}, fmt.Errorf("iac_directory is required")
	}
	logr := logger.GetLogger(ctx).With("fn", "tfExecValidator.validate")

	// Detect which binary to use (tofu or terraform)
	execPath, isTerraform, err := v.detectBinary()
	if err != nil {
		return tfExecValidatorOutput{}, err
	}

	// Create terraform-exec instance
	tf, err := tfexec.NewTerraform(req.IACPath, execPath)
	if err != nil {
		return tfExecValidatorOutput{}, fmt.Errorf("failed to create terraform executor: %w", err)
	}

	// Optimization: Use plugin cache to avoid re-downloading providers
	// We set this environment variable for the tfexec instance
	cacheDir, err := getPluginCacheDir()
	if err == nil && cacheDir != "" {
		// Ensure cache directory exists
		if err := os.MkdirAll(cacheDir, 0755); err == nil {
			// tfexec.SetEnv overwrites the process environment, so we must merge
			// with existing environment variables to preserve PATH (needed for git)
			env := make(map[string]string)
			for _, e := range os.Environ() {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					env[parts[0]] = parts[1]
				}
			}
			env["TF_PLUGIN_CACHE_DIR"] = cacheDir
			if err := tf.SetEnv(env); err != nil {
				logr.Warn("failed to set plugin cache directory", "error", err)
			}
		}
	}

	// Run terraform init (required before validate)
	// Optimization: Backend(false) prevents connecting to remote state, speeding up init
	err = tf.Init(ctx, tfexec.Upgrade(false), tfexec.Backend(false))
	if err != nil {
		return tfExecValidatorOutput{
			IsValid: false,
			Errors:  []string{fmt.Sprintf("terraform init failed: %v", err)},
		}, nil
	}

	// Run terraform validate
	validateOutput, err := tf.Validate(ctx)
	if err != nil {
		// Validation failed - parse the error
		return tfExecValidatorOutput{
			IsValid: false,
			Errors:  []string{fmt.Sprintf("validation failed: %v", err)},
		}, nil
	}

	// Parse validation output
	result := tfExecValidatorOutput{
		IsValid: validateOutput.Valid,
	}

	// Collect errors
	for _, diag := range validateOutput.Diagnostics {
		switch diag.Severity {
		case "error":
			errMsg := v.formatDiagnostic(diag, isTerraform)
			result.Errors = append(result.Errors, errMsg)
		case "warning":
			warnMsg := v.formatDiagnostic(diag, isTerraform)
			result.Warnings = append(result.Warnings, warnMsg)
		}
	}

	if !result.IsValid {
		result.Errors = append([]string{
			fmt.Sprintf("✗ Validation failed with %d error(s)", len(result.Errors)),
		}, result.Errors...)
	}

	return result, nil
}

// detectBinary detects whether to use tofu or terraform
func (v *tfExecValidator) detectBinary() (string, bool, error) {
	// Try tofu first (OpenTofu)
	if path, err := exec.LookPath("tofu"); err == nil {
		return path, false, nil
	}

	// Fall back to terraform
	if path, err := exec.LookPath("terraform"); err == nil {
		return path, true, nil
	}

	return "", false, fmt.Errorf("neither 'tofu' nor 'terraform' binary found in PATH")
}

// formatDiagnostic formats a diagnostic message for display
func (v *tfExecValidator) formatDiagnostic(diag tfjson.Diagnostic, isTerraform bool) string {
	binary := "tofu"
	if isTerraform {
		binary = "terraform"
	}

	msg := fmt.Sprintf("[%s] %s", binary, diag.Summary)

	if diag.Detail != "" {
		msg += fmt.Sprintf(": %s", diag.Detail)
	}

	if diag.Range != nil {
		msg += fmt.Sprintf(" (at %s:%d:%d)",
			diag.Range.Filename,
			diag.Range.Start.Line,
			diag.Range.Start.Column)
	}

	return msg
}

// getPluginCacheDir returns the preferred path for Terraform plugin cache
func getPluginCacheDir() (string, error) {
	// If user already set it, respect that
	if val := os.Getenv("TF_PLUGIN_CACHE_DIR"); val != "" {
		return val, nil
	}

	// Default to ~/.terraform.d/plugin-cache
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".terraform.d", "plugin-cache"), nil
}
