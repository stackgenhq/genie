package toolwrap

import (
	"context"

	"github.com/appcd-dev/genie/pkg/logger"
)

// LoggerMiddleware returns a Middleware that logs the outcome of every
// tool call — debug for success, error for failure. It wraps the
// downstream handler, measures the response, and emits a structured
// log line. Without this middleware, tool call outcomes would be
// invisible in application logs.
func LoggerMiddleware() MiddlewareFunc {
	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			logr := logger.GetLogger(ctx).With("fn", "LoggerMiddleware", "tool", tc.ToolName)

			output, err := next(ctx, tc)

			if err != nil {
				logr.Error("tool call failed", "tool", tc.ToolName, "args", string(tc.Args), "error", err)
			} else {
				logr.Debug("tool call succeeded", "tool", tc.ToolName)
			}

			return output, err
		}
	}
}
