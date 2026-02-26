package toolwrap

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ValidationConfig controls the InputValidationMiddleware.
type ValidationConfig struct {
	Enabled bool `yaml:"enabled,omitempty" toml:"enabled,omitempty"`
}

// InputValidationMiddleware returns a Middleware that validates tool call
// arguments against the tool's declared InputSchema before execution.
// It checks that:
//   - The arguments are valid JSON
//   - Required top-level fields (from the schema) are present
//
// Without this middleware, malformed arguments pass through to the tool
// implementation, leading to cryptic runtime errors instead of clear
// validation messages that help the LLM self-correct.
//
// The validation is intentionally lightweight (required-field check) rather
// than a full JSON Schema validator to keep the dependency footprint small.
func InputValidationMiddleware(toolDeclarations func(name string) *tool.Declaration) MiddlewareFunc {
	return func(next Handler) Handler {
		return func(ctx context.Context, tc *ToolCallContext) (any, error) {
			logr := logger.GetLogger(ctx).With("fn", "InputValidationMiddleware", "tool", tc.ToolName)

			if err := validateArgs(toolDeclarations, tc.ToolName, tc.Args); err != nil {
				logr.Debug("input validation failed", "error", err)
				return nil, err
			}

			return next(ctx, tc)
		}
	}
}

// validateArgs checks that the provided JSON args satisfy the tool's
// declared schema constraints (valid JSON, required fields present).
func validateArgs(declFn func(string) *tool.Declaration, toolName string, args []byte) error {
	if len(args) == 0 {
		return nil // empty args are valid for tools with no parameters
	}

	// Verify valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal(args, &parsed); err != nil {
		return fmt.Errorf("tool %s: invalid JSON arguments: %w", toolName, err)
	}

	if declFn == nil {
		return nil
	}
	decl := declFn(toolName)
	if decl == nil || decl.InputSchema == nil {
		return nil
	}

	// Check required fields.
	for _, req := range decl.InputSchema.Required {
		if _, ok := parsed[req]; !ok {
			return fmt.Errorf("tool %s: missing required argument %q", toolName, req)
		}
	}

	return nil
}
