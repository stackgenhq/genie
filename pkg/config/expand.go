// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package config

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
)

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
	logger := logger.GetLogger(ctx).With("fn", "expandSecrets")
	return os.Expand(input, func(name string) string {
		val, err := sp.GetSecret(ctx, security.GetSecretRequest{
			Name:   name,
			Reason: toolcontext.GetJustification(ctx),
		})
		if err != nil {
			logger.Warn("Failed to get secret", "name", name, "error", err)
			return ""
		}
		return val
	})
}
