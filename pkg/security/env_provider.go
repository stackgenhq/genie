// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package security

import (
	"context"
	"os"

	"github.com/stackgenhq/genie/pkg/logger"
)

// envProvider is a lightweight SecretProvider that reads secrets exclusively
// from environment variables via os.Getenv. It is used as the zero-config
// default when no security section is present in the Genie configuration,
// preserving full backward compatibility with existing .env-based setups.
//
// Without this provider, local development would require an external
// secret store even for simple API key configuration.
type envProvider struct {
	onSecretLookup func(ctx context.Context, req GetSecretRequest)
}

// EnvProviderOption configures an envProvider at construction time.
type EnvProviderOption func(*envProvider)

// WithSecretLookupAudit sets a callback invoked whenever a secret is successfully
// looked up (GetSecret returns a non-empty value). Use it to audit secret access;
// the callback receives the logical secret name only, never the value.
func WithSecretLookupAuditEnv(fn func(ctx context.Context, req GetSecretRequest)) EnvProviderOption {
	return func(e *envProvider) {
		e.onSecretLookup = fn
	}
}

// NewEnvProvider creates a SecretProvider that reads all secrets from
// environment variables. This is the default provider used when no
// runtimevar mappings are configured. Pass WithSecretLookupAuditEnv to
// audit successful lookups (e.g. for compliance).
func NewEnvProvider(opts ...EnvProviderOption) SecretProvider {
	e := &envProvider{}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// GetSecret returns the value of the environment variable named by name.
// Returns an empty string (not an error) when the variable is unset,
// matching the existing os.Getenv behavior that callers expect.
func (e *envProvider) GetSecret(ctx context.Context, req GetSecretRequest) (string, error) {
	val := os.Getenv(req.Name)
	if val != "" && e.onSecretLookup != nil {
		e.auditSecretLookup(ctx, req)
	}
	return val, nil
}

// auditSecretLookup invokes the optional onSecretLookup callback. Call only when
// a non-empty secret value was returned. A panicking callback is recovered and
// logged so secret lookups still succeed.
func (e *envProvider) auditSecretLookup(ctx context.Context, req GetSecretRequest) {
	if e.onSecretLookup == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.GetLogger(ctx).Warn("secret lookup audit callback panicked", "panic", r, "secret_name", req.Name)
		}
	}()
	e.onSecretLookup(ctx, req)
}
