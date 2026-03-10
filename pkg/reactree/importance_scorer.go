// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
)

// ExpertImportanceScorer uses a lightweight LLM call to assign importance
// scores (1-10) to episodes. It uses the efficiency model (cheapest/fastest)
// to keep costs negligible.
//
// This lives in the reactree package (rather than memory) to avoid
// import cycles: memory → expert → reactree → memory.
type ExpertImportanceScorer struct {
	expert expert.Expert
}

// Ensure ExpertImportanceScorer implements memory.ImportanceScorer.
var _ memory.ImportanceScorer = (*ExpertImportanceScorer)(nil)

// NewExpertImportanceScorer creates an ImportanceScorer that uses the given
// expert to score episode importance via a cheap LLM call.
func NewExpertImportanceScorer(exp expert.Expert) *ExpertImportanceScorer {
	return &ExpertImportanceScorer{expert: exp}
}

// importanceScoringPrompt asks the LLM to rate an episode on a 1-10 scale.
const importanceScoringPrompt = `Rate the importance of this AI agent experience on a scale of 1-10.

1-3: Routine, trivial, or obvious (e.g., simple greetings, basic lookups)
4-6: Moderately useful (e.g., successful multi-step tasks, minor failures)
7-9: Highly important (e.g., novel approaches, critical failure patterns, reusable strategies)
10: Critical lesson (e.g., data safety, security patterns, recurring blockers)

Goal: %q
Outcome: %s
Content: %q

Respond with ONLY a single integer from 1 to 10. No other text.`

// Score assigns a 1-10 importance score to the episode via a cheap LLM call.
// Returns 0 if scoring fails (best-effort; never blocks the main flow).
func (s *ExpertImportanceScorer) Score(ctx context.Context, req memory.ImportanceScoringRequest) int {
	logr := logger.GetLogger(ctx).With("fn", "ExpertImportanceScorer.Score")

	// Cap output to prevent large content from bloating the prompt.
	output := req.Output
	const maxOutputChars = 300
	if len(output) > maxOutputChars {
		output = output[:maxOutputChars] + "..."
	}

	prompt := fmt.Sprintf(importanceScoringPrompt, req.Goal, req.Status, output)

	resp, err := s.expert.Do(ctx, expert.Request{
		Message:  prompt,
		TaskType: modelprovider.TaskEfficiency,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
			Silent:            true,
		},
	})
	if err != nil {
		logr.Warn("importance scoring LLM call failed, skipping", "error", err)
		return 0
	}

	if len(resp.Choices) == 0 {
		return 0
	}

	// Parse the integer from the response.
	text := strings.TrimSpace(resp.Choices[len(resp.Choices)-1].Message.Content)
	score, err := strconv.Atoi(text)
	if err != nil {
		// The LLM might add text — try finding a digit.
		score = extractFirstInt(text)
		if score == 0 {
			logr.Warn("could not parse importance score", "raw", text)
			return 0
		}
	}

	// Clamp to 1-10.
	if score < 1 {
		score = 1
	}
	if score > 10 {
		score = 10
	}

	logr.Debug("episode importance scored",
		"goal", req.Goal,
		"score", score,
	)

	return score
}

// extractFirstInt finds the first integer in a string.
// Returns 0 if no integer is found.
func extractFirstInt(s string) int {
	var digits strings.Builder
	started := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			started = true
			continue
		}
		if started {
			break
		}
	}
	if digits.Len() == 0 {
		return 0
	}
	n, _ := strconv.Atoi(digits.String())
	return n
}

// scoreEpisodeImportance is a helper that delegates to the ImportanceScorer
// if available, or returns 0 (unscored). Called from NewAgentNodeFunc
// when storing episodes.
func scoreEpisodeImportance(ctx context.Context, scorer memory.ImportanceScorer, goal, output string, status memory.EpisodeStatus) int {
	if scorer == nil {
		return 0
	}
	return scorer.Score(ctx, memory.ImportanceScoringRequest{
		Goal:   goal,
		Output: output,
		Status: status,
	})
}
