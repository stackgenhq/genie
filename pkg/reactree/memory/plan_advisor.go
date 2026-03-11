// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
)

// Episodes is a domain type wrapping a slice of Episode. It provides
// behavior for summarizing, classifying, and formatting episodes for
// prompt injection — keeping that logic close to the data it operates on.
type Episodes []Episode

// HasFailures returns true if any episode has a failure status.
func (eps Episodes) HasFailures() bool {
	for _, ep := range eps {
		if ep.Status == EpisodeFailure {
			return true
		}
	}
	return false
}

// HasSuccesses returns true if any episode has a success or pending status.
func (eps Episodes) HasSuccesses() bool {
	for _, ep := range eps {
		if ep.Status == EpisodeSuccess || ep.Status == EpisodePending {
			return true
		}
	}
	return false
}

// Summarize formats the episodes into a prompt-ready string. Each episode
// is rendered via its String() method, separated by blank lines.
func (eps Episodes) Summarize() string {
	if len(eps) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, ep := range eps {
		sb.WriteString(ep.String())
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Header returns a contextual header based on the mix of episode statuses.
func (eps Episodes) Header() string {
	if eps.HasFailures() {
		return "⚠️ **Similar tasks have failed before.** Learn from these past attempts:\n\n"
	}
	if eps.HasSuccesses() {
		return "✅ **Similar tasks have succeeded before.** Reference these approaches:\n\n"
	}
	return ""
}

// StepAdvisory holds the advisory context for a single plan step.
type StepAdvisory struct {
	// StepName identifies which plan step this advisory is for.
	StepName string

	// Episodes are the relevant past experiences for this step.
	Episodes Episodes

	// WisdomSection is the formatted wisdom notes (shared across steps).
	WisdomSection string
}

// Format renders the advisory as a prompt-ready string. Returns empty
// if there is nothing to advise on.
func (sa StepAdvisory) Format() string {
	episodeSummary := sa.Episodes.Summarize()
	if episodeSummary == "" && sa.WisdomSection == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Pre-Execution Advisory (from past experiences)\n")

	header := sa.Episodes.Header()
	if header != "" {
		sb.WriteString(header)
	}
	sb.WriteString(episodeSummary)

	if sa.WisdomSection != "" {
		sb.WriteString(sa.WisdomSection)
	}

	return truncateAdvisory(sb.String())
}

// maxAdvisoryRunes caps advisory length to prevent prompt bloat.
const maxAdvisoryRunes = 1200

// truncateAdvisory caps the advisory string to maxAdvisoryRunes.
func truncateAdvisory(s string) string {
	runes := []rune(s)
	if len(runes) <= maxAdvisoryRunes {
		return s
	}
	return string(runes[:maxAdvisoryRunes]) + "\n... (advisory truncated)\n"
}

// PlanAdvisoryResult holds the advisory output for all plan steps.
type PlanAdvisoryResult struct {
	// Advisories maps step name → StepAdvisory.
	Advisories map[string]StepAdvisory
}

// ForStep returns the formatted advisory for a specific step.
// Returns empty string if no advisory exists for that step.
func (r PlanAdvisoryResult) ForStep(stepName string) string {
	if len(r.Advisories) == 0 {
		return ""
	}
	sa, ok := r.Advisories[stepName]
	if !ok {
		return ""
	}
	formatted := sa.Format()
	if formatted == "" {
		return ""
	}
	return fmt.Sprintf("\n\n--- Begin Advisory ---\n%s--- End Advisory ---\n", formatted)
}

// StepsAdvised returns the number of steps that received advisory.
func (r PlanAdvisoryResult) StepsAdvised() int {
	count := 0
	for _, sa := range r.Advisories {
		if sa.Format() != "" {
			count++
		}
	}
	return count
}

// PlanAdvisor consults past experiences (episodic memory and wisdom) to
// generate advisory context for plan steps BEFORE they execute. This
// closes the gap where plan decomposition was "blind" to past outcomes
// — the LLM now sees relevant successes, failures, and lessons when
// executing each step of a multi-step plan.
//
// The interface is defined here (memory package) to avoid import cycles;
// the wiring happens in the reactree package.
//
//counterfeiter:generate . PlanAdvisor
type PlanAdvisor interface {
	// Advise queries past experiences for each step goal and returns
	// advisory context to inject into each step.
	Advise(ctx context.Context, req PlanAdvisoryRequest) PlanAdvisoryResult
}

// PlanAdvisoryRequest holds the inputs for generating plan-level advisory.
type PlanAdvisoryRequest struct {
	// OverallGoal is the parent task's goal — used to retrieve
	// high-level wisdom notes.
	OverallGoal string

	// StepGoals maps step name → step goal text. Each step goal
	// is used to query episodic memory for relevant past experiences.
	StepGoals map[string]string
}

// episodicPlanAdvisor implements PlanAdvisor using EpisodicMemory and
// WisdomStore. It performs pure retrieval — no LLM calls — keeping
// latency low and cost zero.
type episodicPlanAdvisor struct {
	episodic EpisodicMemory
	wisdom   WisdomStore
}

// PlanAdvisorConfig holds dependencies for creating a PlanAdvisor.
type PlanAdvisorConfig struct {
}

// NewPlanAdvisor creates a PlanAdvisor backed by episodic memory and
// an optional wisdom store. Returns a no-op advisor if episodic is nil.
func NewPlanAdvisor(
	// Episodic is the episode store to query for per-step experiences.
	episodic EpisodicMemory,
	// Wisdom is the consolidated wisdom store to query for high-level lessons.
	// Optional — when nil, no wisdom notes are included.
	wisdom WisdomStore) PlanAdvisor {
	return &episodicPlanAdvisor{
		episodic: episodic,
		wisdom:   wisdom,
	}
}

// maxEpisodesPerStep is the maximum number of past episodes to retrieve
// per plan step. Kept small to avoid prompt bloat.
const maxEpisodesPerStep = 2

// maxWisdomNotes is the maximum number of wisdom notes to retrieve for
// the overall plan advisory.
const maxWisdomNotes = 2

// Advise queries episodic memory for each step goal and retrieves
// wisdom notes for the overall plan. Returns a PlanAdvisoryResult.
func (a *episodicPlanAdvisor) Advise(ctx context.Context, req PlanAdvisoryRequest) PlanAdvisoryResult {
	logr := logger.GetLogger(ctx).With("fn", "PlanAdvisor.Advise")

	wisdomSection := a.retrieveWisdom(ctx)
	advisories := make(map[string]StepAdvisory, len(req.StepGoals))

	for stepName, stepGoal := range req.StepGoals {
		episodes := Episodes(a.episodic.RetrieveWeighted(ctx, stepGoal, maxEpisodesPerStep))

		sa := StepAdvisory{
			StepName:      stepName,
			Episodes:      episodes,
			WisdomSection: wisdomSection,
		}

		// Only include steps that have something to advise on.
		if sa.Format() != "" {
			advisories[stepName] = sa
		}
	}

	logr.Info("plan advisory complete",
		"steps_advised", len(advisories),
		"total_steps", len(req.StepGoals),
	)

	return PlanAdvisoryResult{Advisories: advisories}
}

// retrieveWisdom fetches formatted wisdom notes. Returns empty string
// if no wisdom store is configured or no notes exist.
func (a *episodicPlanAdvisor) retrieveWisdom(ctx context.Context) string {
	if a.wisdom == nil {
		return ""
	}
	notes := a.wisdom.RetrieveWisdom(ctx, maxWisdomNotes)
	return FormatWisdomForPrompt(notes)
}

// noOpPlanAdvisor is a no-op implementation for when memory is not configured.
type noOpPlanAdvisor struct{}

func (noOpPlanAdvisor) Advise(_ context.Context, _ PlanAdvisoryRequest) PlanAdvisoryResult {
	return PlanAdvisoryResult{}
}

// NewNoOpPlanAdvisor creates a PlanAdvisor that returns no advisory.
func NewNoOpPlanAdvisor() PlanAdvisor {
	return noOpPlanAdvisor{}
}
