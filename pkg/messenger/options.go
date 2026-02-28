package messenger

import (
	"github.com/stackgenhq/genie/pkg/security"
)

// DefaultMessageBufferSize is the default capacity for the incoming message channel.
const defaultMessageBufferSize = 100

// AdapterConfig holds common configuration used by platform adapters.
// Adapters should embed or reference this struct to benefit from shared options.
type AdapterConfig struct {
	// MessageBufferSize is the capacity of the incoming message channel.
	// Defaults to DefaultMessageBufferSize.
	MessageBufferSize int
	// SecretProvider is optional; when set, adapters (e.g. Google Chat) can use it
	// to resolve OAuth tokens and credentials (logged-in user) instead of service account files.
	SecretProvider security.SecretProvider
}

// DefaultAdapterConfig returns an AdapterConfig with sensible defaults.
func DefaultAdapterConfig() AdapterConfig {
	return AdapterConfig{
		MessageBufferSize: defaultMessageBufferSize,
	}
}

// Option is a functional option for configuring a platform adapter.
type Option func(*AdapterConfig)

// WithMessageBuffer sets the capacity of the incoming message channel.
// Values less than 1 are ignored.
func WithMessageBuffer(size int) Option {
	return func(c *AdapterConfig) {
		if size > 0 {
			c.MessageBufferSize = size
		}
	}
}

// WithSecretProvider sets the secret provider for adapters that support it (e.g. Google Chat).
// When set, the adapter uses the logged-in user OAuth token (from TokenFile/keyring) instead of a service account credentials file.
func WithSecretProvider(sp security.SecretProvider) Option {
	return func(c *AdapterConfig) {
		c.SecretProvider = sp
	}
}

// ApplyOptions applies functional options to the default adapter config.
func ApplyOptions(opts ...Option) AdapterConfig {
	cfg := DefaultAdapterConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
