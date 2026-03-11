// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stackgenhq/genie/pkg/logger"
)

// CircuitBreakerConfig configures the CircuitBreakerMiddleware.
type CircuitBreakerConfig struct {
	// Enabled activates the circuit breaker middleware. Defaults to false.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	// FailureThreshold is the number of consecutive failures before the
	// circuit opens. Defaults to 5 if zero.
	FailureThreshold int `yaml:"failure_threshold,omitempty" toml:"failure_threshold,omitempty,omitzero"`
	// OpenDuration is how long the circuit stays open before transitioning
	// to half-open for a probe. Defaults to 30s if zero.
	OpenDuration time.Duration `yaml:"open_duration,omitempty" toml:"open_duration,omitempty"`
	// HalfOpenMaxCalls is the number of test calls allowed in half-open
	// state before deciding to close or re-open. Defaults to 1 if zero.
	HalfOpenMaxCalls int `yaml:"half_open_max_calls,omitempty" toml:"half_open_max_calls,omitempty,omitzero"`
}

// withDefaults fills zero-valued fields with sensible defaults.
func (c CircuitBreakerConfig) withDefaults() CircuitBreakerConfig {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 5
	}
	if c.OpenDuration <= 0 {
		c.OpenDuration = 30 * time.Second
	}
	if c.HalfOpenMaxCalls <= 0 {
		c.HalfOpenMaxCalls = 1
	}
	return c
}

// CircuitBreakerMW implements the standard three-state circuit
// breaker (closed → open → half-open → closed) using sony/gobreaker.
//
// The TwoStepCircuitBreaker separates the "allow" check from the
// "report outcome" callback, which makes it straightforward to swap
// the in-memory state to a remote backend (Redis, DynamoDB) — you
// only need to replace the inner breaker with one that reads/writes
// counts externally. See [gobreaker.Counts] for available counters.
//
// The optional scope field prefixes breaker keys so that the same tool
// name in different agents gets independent circuit state. This prevents
// policy-denied failures in one agent from opening the circuit globally.
type CircuitBreakerMW struct {
	mu       sync.Mutex
	breakers map[string]*gobreaker.TwoStepCircuitBreaker[any] // per-tool (or per-scope:tool)
	cfg      CircuitBreakerConfig
	scope    string // optional; when set, breaker keys become "scope:toolName"
}

// CircuitBreakerMiddleware creates a per-tool circuit breaker middleware
// backed by sony/gobreaker. Each tool gets its own breaker instance.
func CircuitBreakerMiddleware(cfg CircuitBreakerConfig) *CircuitBreakerMW {
	cfg = cfg.withDefaults()
	return &CircuitBreakerMW{
		breakers: make(map[string]*gobreaker.TwoStepCircuitBreaker[any]),
		cfg:      cfg,
	}
}

// WithScope returns a new CircuitBreakerMW that shares the same breaker
// map and config but uses the given scope as a key prefix. This scopes
// circuit state per-agent so that failures (e.g. policy denials) in one
// agent don't open the circuit for other agents using the same tool.
//
// If scope is empty, 'this' is returned unchanged.
// Without per-agent scoping, a policy that denies tool X in agent A
// would trip the circuit globally, preventing agent B from using tool X
// even though agent B has no such policy.
func (m *CircuitBreakerMW) WithScope(scope string) *CircuitBreakerMW {
	if scope == "" {
		return m
	}
	return &CircuitBreakerMW{
		breakers: m.breakers,
		cfg:      m.cfg,
		scope:    scope,
	}
}

// breakerKey returns the map key for the given tool name, incorporating
// the optional scope prefix.
func (m *CircuitBreakerMW) breakerKey(toolName string) string {
	if m.scope == "" {
		return toolName
	}
	return m.scope + ":" + toolName
}

func (m *CircuitBreakerMW) getBreaker(toolName string) *gobreaker.TwoStepCircuitBreaker[any] {
	key := m.breakerKey(toolName)
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.breakers[key]; ok {
		return b
	}
	cfg := m.cfg
	b := gobreaker.NewTwoStepCircuitBreaker[any](gobreaker.Settings{
		Name:        key,
		MaxRequests: uint32(cfg.HalfOpenMaxCalls),
		Timeout:     cfg.OpenDuration,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return int(counts.ConsecutiveFailures) >= cfg.FailureThreshold
		},
	})
	m.breakers[key] = b
	return b
}

// OpenTools returns the names of tools whose circuit breaker is currently
// in the Open state. The adaptive loop uses this to remove broken tools
// from the LLM's tool set — preventing wasted calls where the LLM tries
// a broken tool only to get rejected by the middleware.
//
// When a scope is set, only breakers matching this scope are considered
// and the scope prefix is stripped from the returned tool names.
func (m *CircuitBreakerMW) OpenTools() []string {
	prefix := ""
	if m.scope != "" {
		prefix = m.scope + ":"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var open []string
	for key, b := range m.breakers {
		if b.State() != gobreaker.StateOpen {
			continue
		}
		// When scoped, only include breakers that belong to this scope.
		if prefix != "" {
			if len(key) > len(prefix) && key[:len(prefix)] == prefix {
				open = append(open, key[len(prefix):])
			}
			continue
		}
		// Unscoped: include all breakers (backward compatibility).
		open = append(open, key)
	}
	return open
}

func (m *CircuitBreakerMW) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		logr := logger.GetLogger(ctx).With("fn", "CircuitBreakerMiddleware", "tool", tc.ToolName)
		cb := m.getBreaker(tc.ToolName)

		done, err := cb.Allow()
		if err != nil {
			logr.Debug("circuit breaker rejected request", "state", cb.State().String())
			return nil, fmt.Errorf(
				"tool %s circuit is %s — the service is currently unavailable. "+
					"Try a different approach or wait before retrying",
				tc.ToolName, cb.State().String())
		}

		output, callErr := next(ctx, tc)

		if callErr != nil && errors.Is(callErr, ErrToolCallRejected) {
			// Do not record a failure for a policy/HITL rejection.
			// Passing nil counts as a success, which avoids tripping the circuit breaker
			// for legitimate user rejections or requests for changes.
			done(nil)
		} else {
			done(callErr)
		}

		if callErr != nil && !errors.Is(callErr, ErrToolCallRejected) {
			counts := cb.Counts()
			logr.Debug("circuit breaker recorded failure",
				"consecutive_failures", counts.ConsecutiveFailures,
				"state", cb.State().String())
		}

		return output, callErr
	}
}
