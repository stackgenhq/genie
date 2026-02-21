package toolwrap

import (
	"context"
	"time"

	"github.com/appcd-dev/genie/pkg/logger"
)

// TimeoutConfig controls the TimeoutMiddleware.
type TimeoutConfig struct {
	// Enabled activates the timeout middleware. Defaults to false.
	Enabled bool `yaml:"enabled" toml:"enabled"`
	// Default is the fallback timeout for all tools.
	Default time.Duration `yaml:"default" toml:"default"`
	// PerTool maps tool names to per-tool timeout overrides.
	PerTool map[string]time.Duration `yaml:"per_tool" toml:"per_tool"`
}

// TimeoutMiddleware returns a Middleware that enforces a maximum execution
// time per tool call. If the tool does not complete within the deadline,
// the context is cancelled and the call returns context.DeadlineExceeded.
// Without this middleware, a misbehaving tool (e.g. a shell command or
// external API call) could hang indefinitely, blocking the agent loop.
func TimeoutMiddleware(d time.Duration) MiddlewareFunc {
	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			logr := logger.GetLogger(ctx).With("fn", "TimeoutMiddleware", "tool", tc.ToolName, "timeout", d.String())
			logr.Debug("applying timeout")

			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, tc)
		}
	}
}

// PerToolTimeoutMiddleware returns a Middleware that enforces per-tool
// timeout overrides. Tools not present in the map use the fallback
// duration. This allows expensive tools (e.g. code execution) to have
// a longer deadline than fast tools (e.g. read_file).
func PerToolTimeoutMiddleware(perTool map[string]time.Duration, fallback time.Duration) MiddlewareFunc {
	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			d := fallback
			if override, ok := perTool[tc.ToolName]; ok {
				d = override
			}
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, tc)
		}
	}
}
