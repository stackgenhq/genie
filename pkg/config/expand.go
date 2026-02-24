/*
Copyright © 2026 StackGen, Inc.
*/

package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/stackgenhq/genie/pkg/security"
)

// sensitiveKeywords lists substrings that typically indicate a secret-ish
// config field. When a placeholder resolves to an empty string for a key
// whose name (lowered) contains one of these keywords, we emit a warning
// so the user gets early feedback instead of a confusing downstream error.
var sensitiveKeywords = []string{
	"token",
	"api_key",
	"apikey",
	"password",
	"authorization",
	"secret",
}

// providersRequiringToken lists provider names that typically require an
// API key to function. If the user configures one of these providers with
// an empty token and no host (self-hosted), it is almost certainly a
// configuration mistake.
var providersRequiringToken = map[string]bool{
	"openai":      true,
	"gemini":      true,
	"anthropic":   true,
	"huggingface": true,
}

// providerTokenInfo holds the minimal fields needed for token validation.
// This avoids importing the modelprovider package and keeps the validation
// logic independent of the full ProviderConfig struct.
type providerTokenInfo struct {
	Provider  string
	ModelName string
	Token     string
	Host      string
}

func (p providerTokenInfo) validate() error {
	if !providersRequiringToken[strings.ToLower(p.Provider)] {
		return nil
	}
	// Skip if host is set — self-hosted endpoints may not need a token.
	if p.Host != "" {
		return nil
	}
	if p.Token == "" {
		return fmt.Errorf("provider %s (model %s) requires an API token: set the token field or configure the secret in [security.secrets]",
			p.Provider, p.ModelName,
		)
	}
	return nil
}

// expandSecrets replaces ${NAME} and $NAME placeholders in input by
// resolving each NAME through the provided SecretProvider. This allows
// config files to reference secrets from any runtimevar-backed store
// (GCP Secret Manager, AWS Secrets Manager, mounted files, etc.)
// instead of relying solely on os.Getenv.
//
// The function mirrors the behaviour of os.ExpandEnv but delegates to
// sp.GetSecret instead of os.Getenv. If GetSecret returns an error the
// placeholder resolves to an empty string (matching os.ExpandEnv
// semantics for missing env vars) so that callers can surface the
// problem via warnUnresolvedSecrets instead of failing outright.
func expandSecrets(ctx context.Context, sp security.SecretProvider, input string) string {
	return os.Expand(input, func(name string) string {
		val, err := sp.GetSecret(ctx, name)
		if err != nil {
			return ""
		}
		return val
	})
}

// warnUnresolvedSecrets scans the raw (post-expansion) config text for
// values that look like they should contain a secret but resolved to
// empty. It inspects lines matching patterns like:
//
//	token = ""          (TOML)
//	token: ""           (YAML)
//	token = ''          (TOML)
//	token: ''           (YAML)
//
// For each match whose key contains a sensitive keyword, it emits a
// slog.Warn so the user knows that a placeholder was not resolved.
//
// Without this function, a typo like ${OPENAI_APY_KEY} would silently
// expand to an empty string, leading to confusing "auth failed" or
// "no providers configured" errors much later in the pipeline.
func warnUnresolvedSecrets(logger *slog.Logger, configPath, rawExpanded string) {
	lines := strings.Split(rawExpanded, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Try to extract key = value or key: value.
		var key, value string
		if idx := strings.Index(trimmed, "="); idx > 0 {
			key = strings.TrimSpace(trimmed[:idx])
			value = strings.TrimSpace(trimmed[idx+1:])
		} else if idx := strings.Index(trimmed, ":"); idx > 0 {
			key = strings.TrimSpace(trimmed[:idx])
			value = strings.TrimSpace(trimmed[idx+1:])
		} else {
			continue
		}

		// Check if value is empty (literally "" or '' or empty).
		isEmpty := value == "" || value == `""` || value == "''"
		if !isEmpty {
			continue
		}

		lowerKey := strings.ToLower(key)
		for _, kw := range sensitiveKeywords {
			if strings.Contains(lowerKey, kw) {
				logger.Warn("config placeholder resolved to empty for secret-like key",
					"key", key,
					"config_path", configPath,
					"hint", "check the placeholder name and ensure the secret is configured in [security.secrets] or as an environment variable",
				)
				break
			}
		}
	}
}
