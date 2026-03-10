// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/stackgenhq/genie/pkg/logger"
)

// RetryConfig configures the RetryMiddleware behaviour.
type RetryConfig struct {
	// Enabled activates the retry middleware. Defaults to false.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	// MaxAttempts is the total number of attempts (including the first call).
	// Defaults to 3 if zero.
	MaxAttempts int `yaml:"max_attempts,omitempty" toml:"max_attempts,omitempty,omitzero"`
	// InitialBackoff is the delay before the first retry. Subsequent retries
	// double this value (exponential backoff). Defaults to 500ms if zero.
	InitialBackoff time.Duration `yaml:"initial_backoff,omitempty" toml:"initial_backoff,omitempty"`
	// MaxBackoff caps the backoff duration. Defaults to 10s if zero.
	MaxBackoff time.Duration `yaml:"max_backoff,omitempty" toml:"max_backoff,omitempty"`
	// Retryable decides whether an error is transient. When nil, all errors
	// are considered retryable. Return false to stop retrying immediately.
	Retryable func(err error) bool `yaml:"-" toml:"-"`
}

// withDefaults fills zero-valued fields with sensible defaults.
func (c RetryConfig) withDefaults() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = 500 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 10 * time.Second
	}
	return c
}

// RetryMiddleware returns a Middleware that automatically retries failed
// tool calls with exponential backoff and jitter, powered by
// cenkalti/backoff/v4. Only errors for which cfg.Retryable returns true
// are retried; non-retryable errors propagate immediately via
// backoff.Permanent. Without this middleware, transient failures (network
// timeouts, 429 responses) require the LLM to manually re-issue the
// call, wasting a turn and tokens.
func RetryMiddleware(cfg RetryConfig) MiddlewareFunc {
	cfg = cfg.withDefaults()

	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			logr := logger.GetLogger(ctx).With("fn", "RetryMiddleware", "tool", tc.ToolName)

			bo := backoff.NewExponentialBackOff()
			bo.InitialInterval = cfg.InitialBackoff
			bo.MaxInterval = cfg.MaxBackoff
			bo.MaxElapsedTime = 0 // we control retries via WithMaxRetries

			// MaxRetries = MaxAttempts - 1 (first call is not a retry).
			bCtx := backoff.WithContext(
				backoff.WithMaxRetries(bo, uint64(cfg.MaxAttempts-1)),
				ctx,
			)

			var result any
			attempt := 0

			err := backoff.Retry(func() error {
				attempt++
				out, callErr := next(ctx, tc)
				if callErr == nil {
					result = out
					return nil
				}

				// Non-retryable → stop immediately.
				if cfg.Retryable != nil && !cfg.Retryable(callErr) {
					return backoff.Permanent(callErr)
				}

				logr.Debug("retrying after transient failure",
					"attempt", attempt,
					"max_attempts", cfg.MaxAttempts,
					"error", callErr,
				)
				return callErr
			}, bCtx)

			if err != nil {
				return nil, fmt.Errorf("tool %s failed after %d attempts: %w",
					tc.ToolName, attempt, err)
			}
			return result, nil
		}
	}
}
