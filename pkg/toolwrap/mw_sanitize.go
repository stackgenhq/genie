package toolwrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
)

// SanitizeMiddlewareConfig controls the OutputSanitizationMiddleware.
type SanitizeMiddlewareConfig struct {
	// Enabled activates the output sanitization middleware.
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
	// Replacement is the string that replaces each redacted occurrence.
	// Defaults to "[REDACTED]".
	Replacement string `yaml:"replacement,omitempty" toml:"replacement,omitempty"`
	// PerTool maps tool names to per-tool redaction patterns (case-insensitive).
	// Tools not in the map pass through unmodified.
	PerTool map[string][]string `yaml:"per_tool,omitempty" toml:"per_tool,omitempty"`
}

// OutputSanitizationMiddleware returns a Middleware that scrubs sensitive
// data from tool outputs before they propagate back up the chain and
// into the LLM context window. Patterns are configured per tool via
// the perTool map (tool name → list of case-insensitive substrings to
// redact). Tools not in the map pass through unmodified. Without this
// middleware, secrets returned by tools (e.g. environment variables,
// config files) would be stored in the conversation history and
// potentially leaked.
func OutputSanitizationMiddleware(perTool map[string][]string, replacement string) MiddlewareFunc {
	if replacement == "" {
		replacement = "[REDACTED]"
	}

	// Pre-lower all patterns for case-insensitive matching.
	lowerPatterns := make(map[string][]string, len(perTool))
	for tool, patterns := range perTool {
		lp := make([]string, len(patterns))
		for i, p := range patterns {
			lp[i] = strings.ToLower(p)
		}
		lowerPatterns[tool] = lp
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			output, err := next(ctx, tc)
			if err != nil || output == nil {
				return output, err
			}

			patterns, ok := lowerPatterns[tc.ToolName]
			if !ok || len(patterns) == 0 {
				return output, nil
			}

			str := fmt.Sprintf("%v", output)
			lowerStr := strings.ToLower(str)
			redacted := false

			for _, pattern := range patterns {
				if !strings.Contains(lowerStr, pattern) {
					continue
				}
				redacted = true
				result := make([]byte, 0, len(str))
				remaining := str
				lowerRemaining := lowerStr
				for {
					idx := strings.Index(lowerRemaining, pattern)
					if idx < 0 {
						result = append(result, remaining...)
						break
					}
					result = append(result, remaining[:idx]...)
					result = append(result, replacement...)
					remaining = remaining[idx+len(pattern):]
					lowerRemaining = lowerRemaining[idx+len(pattern):]
				}
				str = string(result)
				lowerStr = strings.ToLower(str)
			}

			if redacted {
				logr := logger.GetLogger(ctx).With("fn", "OutputSanitizationMiddleware", "tool", tc.ToolName)
				logr.Debug("sensitive patterns redacted from output")
			}

			return str, nil
		}
	}
}
