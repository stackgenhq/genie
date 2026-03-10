// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package expert

// ExpertConfig contains configuration options for token optimization and agent behavior.
// This struct provides presets for different use cases to balance cost vs. capability.
type ExpertConfig struct {
	// MaxLLMCalls is the maximum number of LLM calls per invocation (default: 15)
	MaxLLMCalls int
	// MaxToolIterations is the maximum number of tool iterations per invocation (default: 12)
	MaxToolIterations int
	// MaxHistoryRuns is the maximum number of history messages to include (default: 5)
	MaxHistoryRuns int
	// DisableParallelTools disables parallel tool execution (default: false)
	DisableParallelTools bool
	// ReasoningContentMode
	ReasoningContentMode string
	// PersonaTokenThreshold is the warning limit for the system prompt size (default: 2000)
	PersonaTokenThreshold int
	// Silent disables emitting events to the TUI, useful for background tasks (default: false)
	Silent bool
}

// DefaultExpertConfig returns sensible defaults for token optimization.
// These settings balance cost efficiency with agent capability for typical IaC generation tasks.
func DefaultExpertConfig() ExpertConfig {
	return ExpertConfig{
		MaxLLMCalls:           15,
		MaxToolIterations:     20,
		PersonaTokenThreshold: 2000,
	}
}

// HighPerformanceConfig returns config optimized for complex tasks requiring more iterations.
// Use this for architectures with many components or complex dependencies.
func HighPerformanceConfig() ExpertConfig {
	return ExpertConfig{
		MaxLLMCalls:           25,
		MaxToolIterations:     20,
		PersonaTokenThreshold: 3000,
	}
}

// CostOptimizedConfig returns config optimized for minimal token usage.
// Use this for simple tasks or when cost is the primary concern.
func CostOptimizedConfig() ExpertConfig {
	return ExpertConfig{
		MaxLLMCalls:           8,
		MaxToolIterations:     6,
		PersonaTokenThreshold: 1000,
	}
}
