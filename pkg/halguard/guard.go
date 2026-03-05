// Package halguard provides hallucination detection and mitigation for
// sub-agent outputs in the Genie agent framework.
//
// It implements a tiered verification pipeline inspired by Finch-Zk
// (Goel et al., Aug 2025, arXiv:2508.14314v2) with:
//   - Pre-delegation grounding checks that score goals on a 0–1 confidence
//     scale using multi-signal analysis (structural, semantic, information
//     density) rather than brittle string matching.
//   - Post-execution cross-model consistency verification that detects
//     hallucinated content at a fine-grained block level and applies
//     targeted corrections using a different model family.
//
// The Guard interface is injected into createAgentTool as an optional
// dependency. When nil, sub-agents execute without hallucination checks,
// preserving full backward compatibility.
//
// Model selection strategy (per Finch-Zk findings):
//  1. Collect efficiency-task models first (fast, cheap for verification).
//  2. If fewer than the configured sample count are available, supplement
//     with distinct models from other task types for architectural diversity.
//  3. Cross-model diversity is critical — the paper shows that disabling
//     cross-model sampling significantly degrades detection accuracy.
package halguard

import "context"

//go:generate go tool counterfeiter -generate

// Guard provides pre-delegation and post-execution hallucination checks
// for sub-agent tool calls. Implementations must be safe for concurrent use.
//
//counterfeiter:generate . Guard
type Guard interface {
	// PreCheck scores the sub-agent goal for fabrication risk before execution.
	// Returns a PreCheckResult with a Confidence score between 0.0 (certainly
	// fabricated) and 1.0 (certainly genuine). The caller decides whether to
	// proceed based on the score and a configurable threshold.
	//
	// Uses multi-signal analysis: structural indicators, semantic patterns,
	// information density, and optionally a fast LLM classifier for
	// ambiguous cases. This is more robust than brittle keyword matching.
	PreCheck(ctx context.Context, req PreCheckRequest) (PreCheckResult, error)

	// PostCheck verifies sub-agent output after execution.
	// Returns a VerificationResult with per-block scores and, when contradictions
	// are found, a corrected version of the output. The corrected text preserves
	// accurate blocks and only rewrites contradicted ones.
	PostCheck(ctx context.Context, req PostCheckRequest) (VerificationResult, error)
}
