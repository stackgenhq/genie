/*
Copyright © 2026 StackGen, Inc.
*/

package security

// Config holds the configuration for the secret provider.
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
	Secrets map[string]string `yaml:"secrets" toml:"secrets"`
}

func (c Config) Provider() SecretProvider {
	if len(c.Secrets) > 0 {
		return NewManager(c)
	}
	return NewEnvProvider()
}
