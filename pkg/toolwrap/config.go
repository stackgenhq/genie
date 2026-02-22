package toolwrap

// MiddlewareConfig is the central configuration for all toolwrap
// middlewares. It is embedded in GenieConfig and populated from the
// config file (YAML/TOML). Each middleware has its own sub-struct
// (defined alongside its middleware in the respective mw_*.go file)
// with an Enabled flag. Where applicable, per-tool overrides are
// supported via map[string] fields on the individual configs.
type MiddlewareConfig struct {
	Timeout        TimeoutConfig            `yaml:"timeout" toml:"timeout"`
	RateLimit      RateLimitConfig          `yaml:"rate_limit" toml:"rate_limit"`
	Retry          RetryConfig              `yaml:"retry" toml:"retry"`
	CircuitBreaker CircuitBreakerConfig     `yaml:"circuit_breaker" toml:"circuit_breaker"`
	Concurrency    ConcurrencyConfig        `yaml:"concurrency" toml:"concurrency"`
	Metrics        MetricsConfig            `yaml:"metrics" toml:"metrics"`
	Tracing        TracingConfig            `yaml:"tracing" toml:"tracing"`
	Validation     ValidationConfig         `yaml:"validation" toml:"validation"`
	Sanitize       SanitizeMiddlewareConfig `yaml:"sanitize" toml:"sanitize"`
}

// DefaultMiddlewareConfig returns sensible defaults.
// Circuit breaker is enabled by default to prevent agents from burning
// LLM calls retrying tools that are consistently failing (e.g. DuckDuckGo
// rate-limited). All other optional middlewares start disabled.
func DefaultMiddlewareConfig() MiddlewareConfig {
	return MiddlewareConfig{
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 3, // open after 3 consecutive failures
		},
	}
}
