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
}

// DefaultExpertConfig returns sensible defaults for token optimization.
// These settings balance cost efficiency with agent capability for typical IaC generation tasks.
func DefaultExpertConfig() ExpertConfig {
	return ExpertConfig{
		MaxLLMCalls:       15,
		MaxToolIterations: 20,
		MaxHistoryRuns:    3,
	}
}

// HighPerformanceConfig returns config optimized for complex tasks requiring more iterations.
// Use this for architectures with many components or complex dependencies.
func HighPerformanceConfig() ExpertConfig {
	return ExpertConfig{
		MaxLLMCalls:       25,
		MaxToolIterations: 20,
		MaxHistoryRuns:    8,
	}
}

// CostOptimizedConfig returns config optimized for minimal token usage.
// Use this for simple tasks or when cost is the primary concern.
func CostOptimizedConfig() ExpertConfig {
	return ExpertConfig{
		MaxLLMCalls:       8,
		MaxToolIterations: 6,
		MaxHistoryRuns:    3,
	}
}
