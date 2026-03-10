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
	"strings"

	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
)

// ExpertEpisodeSummarizer uses an LLM to consolidate a batch of episodes
// into a concise wisdom summary. It uses the efficiency model for cost control.
type ExpertEpisodeSummarizer struct {
	expert expert.Expert
}

// Ensure ExpertEpisodeSummarizer implements memory.EpisodeSummarizer.
var _ memory.EpisodeSummarizer = (*ExpertEpisodeSummarizer)(nil)

// NewExpertEpisodeSummarizer creates an EpisodeSummarizer that uses the given
// expert to generate consolidated wisdom summaries.
func NewExpertEpisodeSummarizer(exp expert.Expert) *ExpertEpisodeSummarizer {
	return &ExpertEpisodeSummarizer{expert: exp}
}

// consolidationPromptTemplate is the prompt used to summarize episodes into wisdom.
const consolidationPromptTemplate = `You are summarizing an AI agent's recent experiences into concise lessons learned.

Here are %d recent episodes (goals and outcomes):

%s

Distill these into a bullet-point list of actionable lessons. Rules:
- Maximum 7 bullet points
- Each bullet should be a concise, reusable lesson (1-2 sentences)
- Group similar experiences
- Prioritize failure lessons and novel approaches
- Start each bullet with "- "
- Do NOT include timestamps or raw error text

Example format:
- When deploying to production, always verify the health endpoint before marking complete
- The JIRA API requires pagination for lists over 50 items; use startAt parameter`

// Summarize consolidates a batch of episodes into a concise wisdom summary.
// Returns empty string if summarization fails.
func (s *ExpertEpisodeSummarizer) Summarize(ctx context.Context, episodes []memory.Episode) string {
	logr := logger.GetLogger(ctx).With("fn", "ExpertEpisodeSummarizer.Summarize")

	if len(episodes) == 0 {
		return ""
	}

	// Format episodes for the prompt.
	var episodeBuf strings.Builder
	const maxPerEpisode = 150
	for i, ep := range episodes {
		// Cap each episode's content.
		content := ep.Trajectory
		if ep.Status == memory.EpisodeFailure && ep.Reflection != "" {
			content = ep.Reflection
		}
		if len(content) > maxPerEpisode {
			content = content[:maxPerEpisode] + "..."
		}

		fmt.Fprintf(&episodeBuf, "%d. [%s] Goal: %s → %s\n", i+1, ep.Status, ep.Goal, content)

		// Don't include too many episodes in the prompt.
		if i >= 19 {
			fmt.Fprintf(&episodeBuf, "... and %d more episodes\n", len(episodes)-20)
			break
		}
	}

	prompt := fmt.Sprintf(consolidationPromptTemplate, len(episodes), episodeBuf.String())

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
		logr.Warn("consolidation LLM call failed", "error", err)
		return ""
	}

	summary := ""
	if len(resp.Choices) > 0 {
		summary = strings.TrimSpace(resp.Choices[len(resp.Choices)-1].Message.Content)
	}

	// Cap summary length.
	const maxSummaryRunes = 800
	runes := []rune(summary)
	if len(runes) > maxSummaryRunes {
		summary = string(runes[:maxSummaryRunes]) + "..."
	}

	logr.Info("episode consolidation complete",
		"episode_count", len(episodes),
		"summary_length", len(summary),
	)

	return summary
}
