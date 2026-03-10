// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory

import "context"

// ImportanceScorer assigns a 1-10 importance score to an episode.
// Higher scores indicate novel, critical, or reusable lessons.
// Lower scores indicate routine or trivial experiences.
//
// The interface lives here (memory package) to avoid import cycles;
// the LLM-backed implementation lives in the reactree package.
//
//counterfeiter:generate . ImportanceScorer
type ImportanceScorer interface {
	// Score returns a 1-10 importance score for the given episode.
	// Returns 0 if scoring is unavailable (best-effort).
	Score(ctx context.Context, req ImportanceScoringRequest) int
}

// ImportanceScoringRequest holds the inputs for scoring an episode's importance.
type ImportanceScoringRequest struct {
	// Goal is the task the agent was attempting.
	Goal string
	// Output is the trajectory or reflection text.
	Output string
	// Status is the episode outcome (success, failure, etc.)
	Status EpisodeStatus
}

// noOpImportanceScorer always returns 0 (unscored).
type noOpImportanceScorer struct{}

func (noOpImportanceScorer) Score(_ context.Context, _ ImportanceScoringRequest) int { return 0 }

// NewNoOpImportanceScorer creates an ImportanceScorer that always returns 0.
func NewNoOpImportanceScorer() ImportanceScorer {
	return noOpImportanceScorer{}
}
