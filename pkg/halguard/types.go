package halguard

import (
	"fmt"
	"math"
	"strings"

	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
)

// PreCheckRequest is the input for Guard.PreCheck.
type PreCheckRequest struct {
	// Goal is the sub-agent's goal as specified by the parent agent.
	Goal string

	// Context is the optional context string provided alongside the goal.
	Context string

	// ToolNames lists the tools assigned to the sub-agent.
	ToolNames []string
}

// PreCheckResult carries the grounding assessment for a sub-agent goal.
type PreCheckResult struct {
	// Confidence is the probability that the goal is genuine and grounded
	// in reality. Range: 0.0 (certainly fabricated) to 1.0 (certainly genuine).
	// The caller compares this against a configurable threshold to decide
	// whether to proceed with execution.
	Confidence float64

	// Signals contains the individual signal contributions that produced
	// the confidence score. Each field represents a distinct fabrication
	// signal with its weighted penalty.
	Signals GroundingSignals

	// Summary is a human-readable explanation of the assessment.
	Summary string
}

// GroundingSignals holds the weighted penalties from each fabrication
// detection signal. A value of 0 means the signal did not fire.
// All values are in the range [0, weight_max] where weight_max is
// the signal's maximum contribution to the total penalty.
type GroundingSignals struct {
	// RolePlay detects explicit role-play instructions
	// (e.g. "you are an SRE", "imagine you're", "pretend to be").
	RolePlay float64

	// FabricationPattern detects invented operational data
	// (e.g. "p99 latency spiked from", "error rate jumped from").
	FabricationPattern float64

	// SecondPersonRole detects "You are..." framing at the start of goals.
	SecondPersonRole float64

	// SpecificMetrics detects suspiciously precise numeric claims
	// without tool backing (e.g. "342ms", "2.4%", "1500 req/s").
	SpecificMetrics float64

	// InformationDensity detects an unusually high density of specific
	// technical claims per sentence.
	InformationDensity float64

	// TemporalUrgency detects artificial time pressure language
	// (e.g. "production is down", "immediately", "urgent").
	TemporalUrgency float64
}

// Penalty returns the total fabrication penalty as the sum of all signal
// contributions, capped at 1.0.
func (s GroundingSignals) Penalty() float64 {
	total := s.RolePlay + s.FabricationPattern + s.SecondPersonRole +
		s.SpecificMetrics + s.InformationDensity + s.TemporalUrgency
	return math.Min(1.0, total)
}

// HasAny reports whether any signal fired (has a non-zero value).
func (s GroundingSignals) HasAny() bool {
	return s.Penalty() > 0
}

// MergeScaled adds another GroundingSignals scaled by a factor.
// Used to combine context-field signals at reduced weight.
func (s GroundingSignals) MergeScaled(other GroundingSignals, scale float64) GroundingSignals {
	s.RolePlay += other.RolePlay * scale
	s.FabricationPattern += other.FabricationPattern * scale
	s.SecondPersonRole += other.SecondPersonRole * scale
	s.SpecificMetrics += other.SpecificMetrics * scale
	s.InformationDensity += other.InformationDensity * scale
	s.TemporalUrgency += other.TemporalUrgency * scale
	return s
}

// String returns a human-readable summary of non-zero signals.
func (s GroundingSignals) String() string {
	var parts []string
	if s.RolePlay > 0 {
		parts = append(parts, fmt.Sprintf("role_play=%.2f", s.RolePlay))
	}
	if s.FabricationPattern > 0 {
		parts = append(parts, fmt.Sprintf("fabrication_pattern=%.2f", s.FabricationPattern))
	}
	if s.SecondPersonRole > 0 {
		parts = append(parts, fmt.Sprintf("second_person_role=%.2f", s.SecondPersonRole))
	}
	if s.SpecificMetrics > 0 {
		parts = append(parts, fmt.Sprintf("specific_metrics=%.2f", s.SpecificMetrics))
	}
	if s.InformationDensity > 0 {
		parts = append(parts, fmt.Sprintf("information_density=%.2f", s.InformationDensity))
	}
	if s.TemporalUrgency > 0 {
		parts = append(parts, fmt.Sprintf("temporal_urgency=%.2f", s.TemporalUrgency))
	}
	return strings.Join(parts, ", ")
}

// PostCheckRequest is the input for Guard.PostCheck.
type PostCheckRequest struct {
	// Goal is the original goal given to the sub-agent.
	Goal string

	// Output is the sub-agent's raw text output to verify.
	Output string

	// ToolCallsMade is the number of tool calls the sub-agent executed.
	// A higher count suggests the output is more grounded in real data.
	ToolCallsMade int

	// GenerationModel identifies the model that generated the output
	// (e.g. "claude-sonnet-4-6"). Used to select a different model family
	// for cross-model verification per Finch-Zk §2.5.
	GenerationModel modelprovider.ModelMap
}

// VerificationResult carries the outcome of a PostCheck verification.
type VerificationResult struct {
	// IsFactual is true when no contradictions were detected.
	IsFactual bool

	// CorrectedText holds the corrected output when contradictions were
	// found and targeted corrections were applied. When IsFactual is true,
	// this equals the original output unchanged.
	CorrectedText string

	// BlockScores holds per-block verification results for observability.
	// Only populated for Light and Full tier verifications.
	BlockScores []BlockScore

	// Tier indicates which verification level was applied.
	Tier VerifyTier
}

// BlockScore holds the verification result for a single semantic block
// of the sub-agent's output.
type BlockScore struct {
	// Text is the original block text.
	Text string

	// Label is the consistency verdict: ACCURATE, CONTRADICTION, or NEUTRAL.
	Label BlockLabel

	// Reason explains the verdict (populated for CONTRADICTION and NEUTRAL).
	Reason string
}

type BlockScores []BlockScore

func (scores BlockScores) countContradictions() int {
	n := 0
	for _, s := range scores {
		if s.Label == BlockContradiction {
			n++
		}
	}
	return n
}

// BlockLabel classifies a block's consistency with cross-model samples.
type BlockLabel string

const (
	// BlockAccurate means the block is factually consistent with reference samples.
	BlockAccurate BlockLabel = "ACCURATE"

	// BlockContradiction means the block directly contradicts one or more reference samples.
	BlockContradiction BlockLabel = "CONTRADICTION"

	// BlockNeutral means there is insufficient information for a definitive assessment.
	BlockNeutral BlockLabel = "NEUTRAL"
)

// VerifyTier indicates the level of verification applied to an output.
type VerifyTier string

const (
	// TierNone means no verification was applied (short, tool-grounded output).
	TierNone VerifyTier = "none"

	// TierLight means a single-model sanity check was applied.
	TierLight VerifyTier = "light"

	// TierFull means the full cross-model Finch-Zk pipeline was applied.
	TierFull VerifyTier = "full"
)
