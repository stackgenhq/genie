package toolwrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"golang.org/x/sync/semaphore"
)

// ConcurrencyConfig configures the ConcurrencyMiddleware.
type ConcurrencyConfig struct {
	// Enabled activates the concurrency middleware. Defaults to false.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	// GlobalLimit is the maximum number of concurrent tool calls across
	// all tools. Zero means no global limit.
	GlobalLimit int `yaml:"global_limit,omitempty" toml:"global_limit,omitempty,omitzero"`
	// PerToolLimits maps tool names to per-tool concurrency caps.
	// Tools not in the map are only subject to the global limit.
	PerToolLimits map[string]int `yaml:"per_tool_limits,omitempty" toml:"per_tool_limits,omitempty"`
}

// concurrencyMiddleware limits the number of concurrent tool executions
// using golang.org/x/sync/semaphore weighted semaphores. It supports
// both a global cap and per-tool overrides (bulkhead pattern). When
// the limit is reached, the call blocks until a slot frees up or the
// context is cancelled. Without this middleware, a burst of parallel
// tool calls could exhaust external API connections, file descriptors,
// or memory.
type concurrencyMiddleware struct {
	globalSem *semaphore.Weighted
	mu        sync.Mutex
	perTool   map[string]*semaphore.Weighted
	cfg       ConcurrencyConfig
}

// ConcurrencyMiddleware returns a Middleware that limits concurrent
// tool executions using golang.org/x/sync/semaphore.
func ConcurrencyMiddleware(cfg ConcurrencyConfig) Middleware {
	var globalSem *semaphore.Weighted
	if cfg.GlobalLimit > 0 {
		globalSem = semaphore.NewWeighted(int64(cfg.GlobalLimit))
	}
	return &concurrencyMiddleware{
		globalSem: globalSem,
		perTool:   make(map[string]*semaphore.Weighted),
		cfg:       cfg,
	}
}

func (m *concurrencyMiddleware) getPerToolSem(name string) *semaphore.Weighted {
	limit, ok := m.cfg.PerToolLimits[name]
	if !ok || limit <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if sem, exists := m.perTool[name]; exists {
		return sem
	}
	sem := semaphore.NewWeighted(int64(limit))
	m.perTool[name] = sem
	return sem
}

func (m *concurrencyMiddleware) Wrap(next Handler) Handler {
	return func(ctx context.Context, tc *ToolCallContext) (any, error) {
		logr := logger.GetLogger(ctx).With("fn", "ConcurrencyMiddleware", "tool", tc.ToolName)

		// Acquire per-tool semaphore.
		if sem := m.getPerToolSem(tc.ToolName); sem != nil {
			logr.Debug("acquiring per-tool semaphore")
			if err := sem.Acquire(ctx, 1); err != nil {
				return nil, fmt.Errorf("tool %s concurrency wait cancelled: %w",
					tc.ToolName, err)
			}
			defer sem.Release(1)
		}

		// Acquire global semaphore.
		if m.globalSem != nil {
			logr.Debug("acquiring global semaphore")
			if err := m.globalSem.Acquire(ctx, 1); err != nil {
				return nil, fmt.Errorf("global concurrency wait cancelled: %w", err)
			}
			defer m.globalSem.Release(1)
		}

		return next(ctx, tc)
	}
}
