package messenger

// DefaultMessageBufferSize is the default capacity for the incoming message channel.
const DefaultMessageBufferSize = 100

// AdapterConfig holds common configuration used by platform adapters.
// Adapters should embed or reference this struct to benefit from shared options.
type AdapterConfig struct {
	// MessageBufferSize is the capacity of the incoming message channel.
	// Defaults to DefaultMessageBufferSize.
	MessageBufferSize int
}

// DefaultAdapterConfig returns an AdapterConfig with sensible defaults.
func DefaultAdapterConfig() AdapterConfig {
	return AdapterConfig{
		MessageBufferSize: DefaultMessageBufferSize,
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

// ApplyOptions applies functional options to the default adapter config.
func ApplyOptions(opts ...Option) AdapterConfig {
	cfg := DefaultAdapterConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
