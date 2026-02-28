/*
Copyright © 2026 StackGen, Inc.
*/

package security

import "context"

// Config holds the configuration for the secret provider and cryptographic policy.
// When present in the Genie config, it controls how secrets are resolved
// at runtime. Without this configuration, the application falls back to
// reading all secrets from environment variables via os.Getenv.
type Config struct {
	// Secrets maps logical secret names (matching the env var names used
	// throughout the codebase, e.g. "OPENAI_API_KEY") to runtimevar URLs.
	// Any secret name NOT present in this map falls back to os.Getenv.
	// Examples:
	//   "OPENAI_API_KEY": "gcpsecretmanager://projects/p/secrets/openai-key?decoder=string"
	//   "ANTHROPIC_API_KEY": "awssecretsmanager://anthropic-api-key?region=us-east-2&decoder=string"
	//   "SLACK_BOT_TOKEN": "file:///run/secrets/slack-token?decoder=string"
	Secrets map[string]string `yaml:"secrets,omitempty" toml:"secrets,omitempty"`

	// Crypto configures key lengths and algorithm policy (NIST 2030; weak algorithms are always disabled).
	Crypto CryptoConfig `yaml:"crypto,omitempty" toml:"crypto,omitempty"`
}

func (c Config) Provider(ctx context.Context) SecretProvider {
	if len(c.Secrets) > 0 {
		return NewManager(ctx, c)
	}
	return NewEnvProvider()
}
