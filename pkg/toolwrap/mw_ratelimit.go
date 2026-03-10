// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"golang.org/x/time/rate"
)

// RateLimitConfig specifies rate limits for the RateLimitMiddleware.
type RateLimitConfig struct {
	// Enabled activates the rate limit middleware. Defaults to false.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	// GlobalRatePerMinute is the maximum number of tool calls per minute
	// across all tools. Zero means no global limit.
	GlobalRatePerMinute float64 `yaml:"global_rate_per_minute,omitempty" toml:"global_rate_per_minute,omitempty,omitzero"`
	// PerToolRatePerMinute maps tool names to per-tool rate limits.
	// Tools not in the map use the global limit. Zero entries are ignored.
	PerToolRatePerMinute map[string]float64 `yaml:"per_tool_rate_per_minute,omitempty" toml:"per_tool_rate_per_minute,omitempty"`
}

// rateLimitMiddleware throttles tool calls using golang.org/x/time/rate
// token-bucket limiters. It supports both a global rate limit and
// per-tool overrides. When the bucket is exhausted, the call is rejected
// immediately with an error telling the LLM to slow down. Without this
// middleware, a runaway agent could exhaust external API quotas or burn
// through token budgets in seconds.
type rateLimitMiddleware struct {
	globalLimiter *rate.Limiter
	mu            sync.Mutex
	perTool       map[string]*rate.Limiter
	cfg           RateLimitConfig
}

// RateLimitMiddleware returns a Middleware that throttles tool calls
// using golang.org/x/time/rate token-bucket limiters. It supports both
// a global rate limit and per-tool overrides (bulkhead pattern).
func RateLimitMiddleware(cfg RateLimitConfig) Middleware {
	var globalLimiter *rate.Limiter
	if cfg.GlobalRatePerMinute > 0 {
		// rate.Limit is events per second; convert from per-minute.
		globalLimiter = rate.NewLimiter(
			rate.Limit(cfg.GlobalRatePerMinute/60),
			int(cfg.GlobalRatePerMinute), // burst = full bucket
		)
	}
	return &rateLimitMiddleware{
		globalLimiter: globalLimiter,
		perTool:       make(map[string]*rate.Limiter),
		cfg:           cfg,
	}
}

func (m *rateLimitMiddleware) getPerToolLimiter(name string) *rate.Limiter {
	rpm, ok := m.cfg.PerToolRatePerMinute[name]
	if !ok || rpm <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if lim, exists := m.perTool[name]; exists {
		return lim
	}
	lim := rate.NewLimiter(rate.Limit(rpm/60), int(rpm))
	m.perTool[name] = lim
	return lim
}

func (m *rateLimitMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		logr := logger.GetLogger(ctx).With("fn", "RateLimitMiddleware", "tool", tc.ToolName)

		// Check per-tool limit first.
		if lim := m.getPerToolLimiter(tc.ToolName); lim != nil {
			if !lim.Allow() {
				logr.Debug("per-tool rate limit exceeded")
				return nil, fmt.Errorf(
					"tool %s is rate-limited. Wait a moment before calling this tool again",
					tc.ToolName)
			}
		}

		// Check global limit.
		if m.globalLimiter != nil {
			if !m.globalLimiter.Allow() {
				logr.Debug("global rate limit exceeded")
				return nil, fmt.Errorf(
					"global tool rate limit exceeded. Slow down — too many tool calls per minute")
			}
		}

		return next(ctx, tc)
	}
}
