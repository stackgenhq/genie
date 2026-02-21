package toolwrap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/sony/gobreaker/v2"
)

// CircuitBreakerConfig configures the CircuitBreakerMiddleware.
type CircuitBreakerConfig struct {
	// Enabled activates the circuit breaker middleware. Defaults to false.
	Enabled bool `yaml:"enabled" toml:"enabled"`
	// FailureThreshold is the number of consecutive failures before the
	// circuit opens. Defaults to 5 if zero.
	FailureThreshold int `yaml:"failure_threshold" toml:"failure_threshold"`
	// OpenDuration is how long the circuit stays open before transitioning
	// to half-open for a probe. Defaults to 30s if zero.
	OpenDuration time.Duration `yaml:"open_duration" toml:"open_duration"`
	// HalfOpenMaxCalls is the number of test calls allowed in half-open
	// state before deciding to close or re-open. Defaults to 1 if zero.
	HalfOpenMaxCalls int `yaml:"half_open_max_calls" toml:"half_open_max_calls"`
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

// circuitBreakerMiddleware implements the standard three-state circuit
// breaker (closed → open → half-open → closed) using sony/gobreaker.
//
// The TwoStepCircuitBreaker separates the "allow" check from the
// "report outcome" callback, which makes it straightforward to swap
// the in-memory state to a remote backend (Redis, DynamoDB) — you
// only need to replace the inner breaker with one that reads/writes
// counts externally. See [gobreaker.Counts] for available counters.
type circuitBreakerMiddleware struct {
	mu       sync.Mutex
	breakers map[string]*gobreaker.TwoStepCircuitBreaker[any] // per-tool
	cfg      CircuitBreakerConfig
}

// CircuitBreakerMiddleware creates a per-tool circuit breaker middleware
// backed by sony/gobreaker. Each tool gets its own breaker instance.
func CircuitBreakerMiddleware(cfg CircuitBreakerConfig) Middleware {
	cfg = cfg.withDefaults()
	return &circuitBreakerMiddleware{
		breakers: make(map[string]*gobreaker.TwoStepCircuitBreaker[any]),
		cfg:      cfg,
	}
}

func (m *circuitBreakerMiddleware) getBreaker(name string) *gobreaker.TwoStepCircuitBreaker[any] {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.breakers[name]; ok {
		return b
	}
	cfg := m.cfg
	b := gobreaker.NewTwoStepCircuitBreaker[any](gobreaker.Settings{
		Name:        name,
		MaxRequests: uint32(cfg.HalfOpenMaxCalls),
		Timeout:     cfg.OpenDuration,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return int(counts.ConsecutiveFailures) >= cfg.FailureThreshold
		},
	})
	m.breakers[name] = b
	return b
}

func (m *circuitBreakerMiddleware) Wrap(next Handler) Handler {
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
		done(callErr)

		if callErr != nil {
			counts := cb.Counts()
			logr.Debug("circuit breaker recorded failure",
				"consecutive_failures", counts.ConsecutiveFailures,
				"state", cb.State().String())
		}

		return output, callErr
	}
}
