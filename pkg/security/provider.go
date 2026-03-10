// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package security provides a centralized SecretProvider abstraction backed
// by gocloud.dev/runtimevar. It replaces scattered os.Getenv calls with a
// single injectable interface that can resolve secrets from GCP Secret
// Manager, AWS Secrets Manager, etcd, HashiCorp Vault, local files, or
// plain environment variables — controlled entirely by URL scheme.
//
// A singleton SecretProvider is created during app.Bootstrap and injected
// into every component that needs secrets. Without this package, each
// component would independently read os.Getenv, making it impossible to
// use external secret stores without modifying every call site.
package security

import "context"

//go:generate go tool counterfeiter -generate

// SecretProvider defines the interface for retrieving secret values by name.
// All secret resolution in the application flows through this interface,
// enabling portable secret backends (GCP, AWS, Vault, env, etc.) without
// changing consuming code.
//
// Without this interface, every component that needs a secret would
// directly couple to os.Getenv or a specific cloud SDK, making it
// impossible to swap backends or test secret-dependent code in isolation.
//
//counterfeiter:generate . SecretProvider
type SecretProvider interface {
	// GetSecret retrieves the plaintext value of the named secret.
	// If the secret is not found in the configured backend, the
	// implementation may fall back to os.Getenv(name).
	// Returns an empty string (not an error) when the secret is
	// simply absent — callers treat missing secrets as unconfigured
	// features, not failures.
	GetSecret(ctx context.Context, req GetSecretRequest) (string, error)
}

type GetSecretRequest struct {
	Name   string
	Reason string
}
