// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package halguard

// Config holds the tuning parameters for hallucination guard behaviour.
// Zero values use sensible defaults. This struct is embedded in
// config.GenieConfig and deserialized from the [halguard] section of
// genie.toml / genie.yaml.
type Config struct {
	// LightThresholdChars is the output length above which the light
	// verification tier is applied. Default: 200.
	LightThresholdChars int `yaml:"light_threshold_chars,omitempty" toml:"light_threshold_chars,omitempty"`

	// FullThresholdChars is the output length above which the full
	// cross-model Finch-Zk verification tier is applied. Default: 500.
	FullThresholdChars int `yaml:"full_threshold_chars,omitempty" toml:"full_threshold_chars,omitempty"`

	// EnablePreCheck controls whether pre-delegation grounding checks run.
	// Default: true.
	EnablePreCheck bool `yaml:"enable_pre_check,omitempty" toml:"enable_pre_check,omitempty"`

	// EnablePostCheck controls whether post-execution verification runs.
	// Default: true.
	EnablePostCheck bool `yaml:"enable_post_check,omitempty" toml:"enable_post_check,omitempty"`

	// CrossModelSamples is the number of cross-model samples to generate
	// for full verification. Finch-Zk shows 3 samples with batch judging
	// maintains accuracy while keeping cost manageable. Default: 3.
	CrossModelSamples int `yaml:"cross_model_samples,omitempty" toml:"cross_model_samples,omitempty"`

	// MaxBlocksToJudge caps the number of blocks sent for cross-consistency
	// judging to limit cost on very long outputs. Default: 20.
	MaxBlocksToJudge int `yaml:"max_blocks_to_judge,omitempty" toml:"max_blocks_to_judge,omitempty"`

	// PreCheckThreshold is the confidence score below which a sub-agent
	// goal is rejected as likely fabricated. Range: (0.0–1.0]. Default: 0.4.
	// Lower = more permissive, higher = more strict. A value of 0 (or an
	// omitted field) is treated as "unset" and causes the default to be used.
	PreCheckThreshold float64 `yaml:"pre_check_threshold,omitempty" toml:"pre_check_threshold,omitempty"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		LightThresholdChars: 200,
		FullThresholdChars:  500,
		EnablePreCheck:      true,
		EnablePostCheck:     true,
		CrossModelSamples:   3,
		MaxBlocksToJudge:    20,
		PreCheckThreshold:   0.4,
	}
}

// defaults returns a Config with sensible defaults applied to zero-value fields.
func (c Config) defaults() Config {
	defaultConfig := DefaultConfig()
	if c.LightThresholdChars == 0 {
		c.LightThresholdChars = defaultConfig.LightThresholdChars
	}
	if c.FullThresholdChars == 0 {
		c.FullThresholdChars = defaultConfig.FullThresholdChars
	}
	if c.CrossModelSamples == 0 {
		c.CrossModelSamples = defaultConfig.CrossModelSamples
	}
	if c.MaxBlocksToJudge == 0 {
		c.MaxBlocksToJudge = defaultConfig.MaxBlocksToJudge
	}
	if c.PreCheckThreshold == 0 {
		c.PreCheckThreshold = defaultConfig.PreCheckThreshold
	}
	return c
}

// Option configures optional behaviour on the Guard.
type Option func(*Config)

// WithConfig applies a Config struct, overriding only non-zero numeric
// fields and always applying the bool flags. This is the preferred option
// when the config comes from genie.toml deserialization.
//
// Because TOML/YAML omitempty cannot distinguish "field absent" from
// "field set to zero/false", callers who want to explicitly disable
// pre-check or post-check must set the field in the config file:
//
//	[halguard]
//	enable_pre_check = false
//
// When no [halguard] section exists, all fields are zero and the
// defaults from New() are preserved.
func WithConfig(cfg Config) Option {
	return func(c *Config) {
		if cfg.LightThresholdChars > 0 {
			c.LightThresholdChars = cfg.LightThresholdChars
		}
		if cfg.FullThresholdChars > 0 {
			c.FullThresholdChars = cfg.FullThresholdChars
		}
		if cfg.CrossModelSamples > 0 {
			c.CrossModelSamples = cfg.CrossModelSamples
		}
		if cfg.MaxBlocksToJudge > 0 {
			c.MaxBlocksToJudge = cfg.MaxBlocksToJudge
		}
		if cfg.PreCheckThreshold > 0 {
			c.PreCheckThreshold = cfg.PreCheckThreshold
		}
		// Bool fields: only override when the incoming config has at least
		// one non-bool field explicitly set (= user provided a [halguard]
		// section with meaningful content). This prevents a config struct
		// with only non-bool defaults (e.g. just cross_model_samples = 5)
		// from accidentally disabling both checks via zero-value bools.
		hasExplicitNonBool := cfg.LightThresholdChars > 0 ||
			cfg.FullThresholdChars > 0 ||
			cfg.CrossModelSamples > 0 ||
			cfg.MaxBlocksToJudge > 0 ||
			cfg.PreCheckThreshold > 0
		if hasExplicitNonBool {
			c.EnablePreCheck = cfg.EnablePreCheck
			c.EnablePostCheck = cfg.EnablePostCheck
		}
	}
}

// WithLightThreshold sets the character count above which light verification is applied.
func WithLightThreshold(chars int) Option {
	return func(c *Config) {
		c.LightThresholdChars = chars
	}
}

// WithFullThreshold sets the character count above which full Finch-Zk verification is applied.
func WithFullThreshold(chars int) Option {
	return func(c *Config) {
		c.FullThresholdChars = chars
	}
}

// WithCrossModelSamples sets the number of cross-model samples to generate.
func WithCrossModelSamples(n int) Option {
	return func(c *Config) {
		c.CrossModelSamples = n
	}
}

// WithPreCheck enables or disables the pre-delegation grounding check.
func WithPreCheck(enable bool) Option {
	return func(c *Config) {
		c.EnablePreCheck = enable
	}
}

// WithPostCheck enables or disables the post-execution output verification.
func WithPostCheck(enable bool) Option {
	return func(c *Config) {
		c.EnablePostCheck = enable
	}
}
