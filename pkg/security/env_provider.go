/*
Copyright © 2026 StackGen, Inc.
*/

package security

import (
	"context"
	"os"
)

// envProvider is a lightweight SecretProvider that reads secrets exclusively
// from environment variables via os.Getenv. It is used as the zero-config
// default when no security section is present in the Genie configuration,
// preserving full backward compatibility with existing .env-based setups.
//
// Without this provider, local development would require an external
// secret store even for simple API key configuration.
type envProvider struct{}

// NewEnvProvider creates a SecretProvider that reads all secrets from
// environment variables. This is the default provider used when no
// runtimevar mappings are configured.
func NewEnvProvider() SecretProvider {
	return &envProvider{}
}

// GetSecret returns the value of the environment variable named by name.
// Returns an empty string (not an error) when the variable is unset,
// matching the existing os.Getenv behavior that callers expect.
func (e *envProvider) GetSecret(_ context.Context, name string) (string, error) {
	return os.Getenv(name), nil
}
