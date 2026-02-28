package reactree

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ActionValidator defines the middleware interface for deterministic or LLM-based branch pruning.
type ActionValidator interface {
	// Validate checks if the tool call with its JSON arguments is permitted.
	// Returns an error if the action is rejected/pruned.
	Validate(ctx context.Context, toolName string, jsonArgs []byte) error
}

// ValidatingToolWrapper wraps a tool.Tool with an ActionValidator.
type ValidatingToolWrapper struct {
	tool.Tool
	validator ActionValidator
}

// WrapWithValidator wraps the provided tool with the given validator.
// If validator is nil, it returns the tool unmodified.
func WrapWithValidator(t tool.Tool, validator ActionValidator) tool.Tool {
	if validator == nil {
		return t
	}
	return &ValidatingToolWrapper{Tool: t, validator: validator}
}

// Call wraps the tool Call method by running the validator first.
func (v *ValidatingToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if err := v.validator.Validate(ctx, v.Tool.Declaration().Name, jsonArgs); err != nil {
		logger.GetLogger(ctx).Warn("action pruned by validator", "tool", v.Tool.Declaration().Name, "error", err)
		return nil, fmt.Errorf("action pruned by critic: %w", err)
	}
	if ct, ok := v.Tool.(tool.CallableTool); ok {
		return ct.Call(ctx, jsonArgs)
	}
	return nil, fmt.Errorf("tool %s is not callable", v.Tool.Declaration().Name)
}

// StreamableCall wraps the tool StreamableCall method by running the validator first.
func (v *ValidatingToolWrapper) StreamableCall(ctx context.Context, jsonArgs []byte) (*tool.StreamReader, error) {
	if err := v.validator.Validate(ctx, v.Tool.Declaration().Name, jsonArgs); err != nil {
		logger.GetLogger(ctx).Warn("action pruned by validator (streamable)", "tool", v.Tool.Declaration().Name, "error", err)
		return nil, fmt.Errorf("action pruned by critic: %w", err)
	}
	if st, ok := v.Tool.(tool.StreamableTool); ok {
		return st.StreamableCall(ctx, jsonArgs)
	}
	return nil, fmt.Errorf("tool %s is not streamable", v.Tool.Declaration().Name)
}

// DeterministicValidator implements ActionValidator by checking against safe policies.
type DeterministicValidator struct {
	BlockedTools []string
}

// Ensure DeterministicValidator implements ActionValidator
var _ ActionValidator = (*DeterministicValidator)(nil)

// NewDeterministicValidator creates a validator that rejects tools in the BlockedTools list
func NewDeterministicValidator(blockedTools []string) *DeterministicValidator {
	return &DeterministicValidator{BlockedTools: blockedTools}
}

func (d *DeterministicValidator) Validate(ctx context.Context, toolName string, jsonArgs []byte) error {
	for _, bt := range d.BlockedTools {
		// Example simplistic deterministic constraint where certain tool operations could be entirely blocked over strict conditions
		if strings.EqualFold(toolName, bt) {
			return fmt.Errorf("tool execution prohibited by deterministic policy: tool=%q", toolName)
		}
	}
	return nil
}
