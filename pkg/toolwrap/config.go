// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

// MiddlewareConfig is the central configuration for all toolwrap
// middlewares. It is embedded in GenieConfig and populated from the
// config file (YAML/TOML). Each middleware has its own sub-struct
// (defined alongside its middleware in the respective mw_*.go file)
// with an Enabled or Disabled flag. Where applicable, per-tool overrides are
// supported via map[string] fields on the individual configs.
type MiddlewareConfig struct {
	ContextModeConfig ContextModeConfig        `yaml:"context_mode,omitempty" toml:"context_mode,omitempty"`
	Timeout           TimeoutConfig            `yaml:"timeout,omitempty" toml:"timeout,omitempty"`
	RateLimit         RateLimitConfig          `yaml:"rate_limit,omitempty" toml:"rate_limit,omitempty"`
	Retry             RetryConfig              `yaml:"retry,omitempty" toml:"retry,omitempty"`
	CircuitBreaker    CircuitBreakerConfig     `yaml:"circuit_breaker,omitempty" toml:"circuit_breaker,omitempty"`
	Concurrency       ConcurrencyConfig        `yaml:"concurrency,omitempty" toml:"concurrency,omitempty"`
	Metrics           MetricsConfig            `yaml:"metrics,omitempty" toml:"metrics,omitempty"`
	Tracing           TracingConfig            `yaml:"tracing,omitempty" toml:"tracing,omitempty"`
	Validation        ValidationConfig         `yaml:"validation,omitempty" toml:"validation,omitempty"`
	Sanitize          SanitizeMiddlewareConfig `yaml:"sanitize,omitempty" toml:"sanitize,omitempty"`
	LoopDetection     LoopDetectionConfig      `yaml:"loop_detection,omitempty" toml:"loop_detection,omitempty"`
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
