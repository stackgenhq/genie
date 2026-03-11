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
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
)

// EpisodeSummarizer generates a consolidated summary from a batch of episodes.
// The interface is defined in the memory package to avoid import cycles;
// the LLM-backed implementation lives in the reactree package.
//
//counterfeiter:generate . EpisodeSummarizer
type EpisodeSummarizer interface {
	// Summarize takes a batch of episodes and produces a concise wisdom summary.
	// Returns the summary text, or empty string if summarization fails.
	Summarize(ctx context.Context, episodes []Episode) string
}

// EpisodeConsolidator reads recent raw episodes, groups them, summarizes
// them into wisdom notes, and stores the results. It is designed to be
// called periodically (e.g., daily via cron or on startup).
type EpisodeConsolidator struct {
	episodic    EpisodicMemory
	wisdom      WisdomStore
	summarizer  EpisodeSummarizer
	lookbackHrs int
}

// EpisodeConsolidatorConfig holds dependencies for creating a consolidator.
type EpisodeConsolidatorConfig struct {
	// Episodic is the source of raw episodes to consolidate.
	Episodic EpisodicMemory

	// Wisdom is the destination for consolidated wisdom notes.
	Wisdom WisdomStore

	// Summarizer generates the consolidated summary text.
	Summarizer EpisodeSummarizer

	// LookbackHours is how far back to look for episodes (default: 24).
	LookbackHours int
}

// NewEpisodeConsolidator creates a consolidator. Returns nil if any essential
// dependency is missing.
func NewEpisodeConsolidator(cfg EpisodeConsolidatorConfig) *EpisodeConsolidator {
	if cfg.Episodic == nil || cfg.Wisdom == nil || cfg.Summarizer == nil {
		return nil
	}
	lookback := cfg.LookbackHours
	if lookback <= 0 {
		lookback = 24
	}
	return &EpisodeConsolidator{
		episodic:    cfg.Episodic,
		wisdom:      cfg.Wisdom,
		summarizer:  cfg.Summarizer,
		lookbackHrs: lookback,
	}
}

// Consolidate reads recent episodes, summarizes them into a single wisdom
// note, and stores it. It is idempotent for the same period — if called
// multiple times in the same day, only the first call produces a new note.
//
// Returns the number of episodes consolidated, or 0 if nothing was done.
func (c *EpisodeConsolidator) Consolidate(ctx context.Context) int {
	if c == nil {
		return 0
	}

	logr := logger.GetLogger(ctx).With("fn", "EpisodeConsolidator.Consolidate")
	period := time.Now().UTC().Format("2006-01-02")

	// Check if we already consolidated for this period.
	existing := c.wisdom.RetrieveWisdom(ctx, 5)
	for _, note := range existing {
		if note.Period == period {
			logr.Info("wisdom note already exists for period, skipping", "period", period)
			return 0
		}
	}

	// Retrieve a generous batch of recent episodes.
	// We use the standard Retrieve (not weighted) since we want all
	// recent episodes regardless of similarity scoring.
	//
	// TODO(perf): Retrieve("", 50) triggers an unbounded LIKE '%%' in the
	// DB-backed SearchMemories (no SQL LIMIT). Consider adding a dedicated
	// RetrieveRecent(ctx, limit) API or extending the backend search with
	// server-side limiting so daily consolidation stays O(limit) as the
	// memory table grows.
	episodes := c.episodic.Retrieve(ctx, "", 50)
	if len(episodes) == 0 {
		logr.Info("no episodes to consolidate")
		return 0
	}

	// Filter to episodes from the lookback window.
	cutoff := time.Now().Add(-time.Duration(c.lookbackHrs) * time.Hour)
	var recent []Episode
	for _, ep := range episodes {
		if ep.CreatedAt.After(cutoff) {
			recent = append(recent, ep)
		}
	}

	if len(recent) == 0 {
		logr.Info("no recent episodes within lookback window", "lookback_hrs", c.lookbackHrs)
		return 0
	}

	logr.Info("consolidating episodes",
		"total_episodes", len(episodes),
		"recent_episodes", len(recent),
		"period", period,
	)

	// Generate the wisdom summary.
	summary := c.summarizer.Summarize(ctx, recent)
	if summary == "" {
		logr.Warn("summarizer returned empty summary, skipping consolidation")
		return 0
	}

	// Prepend the date context.
	wisdomText := fmt.Sprintf("On %s, you learned:\n%s", period, summary)

	c.wisdom.StoreWisdom(ctx, WisdomNote{
		Summary:      wisdomText,
		Period:       period,
		EpisodeCount: len(recent),
	})

	logr.Info("wisdom note stored",
		"period", period,
		"episode_count", len(recent),
		"summary_length", len(wisdomText),
	)

	return len(recent)
}

// FormatWisdomForPrompt renders a list of wisdom notes as a prompt section.
// Used by buildAgentPrompt to inject consolidated lessons into context.
func FormatWisdomForPrompt(notes []WisdomNote) string {
	if len(notes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Consolidated Lessons\n")
	sb.WriteString("These are distilled lessons from your recent experiences:\n\n")
	for _, note := range notes {
		sb.WriteString(note.Summary)
		sb.WriteString("\n\n")
	}
	return sb.String()
}
