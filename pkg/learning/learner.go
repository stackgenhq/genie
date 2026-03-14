// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package learning provides autonomous skill distillation from completed
// agent sessions. After a task is completed successfully, the Learner
// reviews what happened and, if the task is novel enough, synthesises a
// reusable skill following the agentskills.io standard.
//
// Learned skills are persisted in two places:
//  1. MutableRepository — filesystem-backed, used by the skills framework.
//  2. Vector store — used for semantic search so the orchestrator can
//     discover relevant skills via memory_search before starting new tasks.
package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/llmutil"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/skills"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// minimumNoveltyScore is the threshold (1-10) above which a task is
// considered novel enough to warrant distillation into a skill.
const minimumNoveltyScore = 7

// skillMetadataType is the metadata type tag used when storing skills
// in the vector store. This allows memory_search to filter/distinguish
// learned skills from other memories.
const skillMetadataType = "learned_skill"

// Learner reviews completed sessions and distills reusable skills.
type Learner struct {
	expert      expert.Expert
	skillRepo   *skills.MutableRepository
	vectorStore vector.IStore
	auditor     audit.Auditor
}

// NewLearner creates a Learner backed by the given expert, skill repository,
// vector store (for discoverability), and auditor.
func NewLearner(
	exp expert.Expert,
	skillRepo *skills.MutableRepository,
	vectorStore vector.IStore,
	auditor audit.Auditor,
) *Learner {
	return &Learner{
		expert:      exp,
		skillRepo:   skillRepo,
		vectorStore: vectorStore,
		auditor:     auditor,
	}
}

// LearnRequest contains the context needed for a learning attempt.
type LearnRequest struct {
	// Goal is the original user question / task.
	Goal string

	// Output is the final answer / result from the agent.
	Output string

	// ToolsUsed lists the names of tools that were called during execution.
	ToolsUsed []string
}

// skillProposal is the JSON structure returned by the LLM when it
// decides that a task should be distilled into a skill.
type skillProposal struct {
	ShouldCreate bool   `json:"should_create"`
	NoveltyScore int    `json:"novelty_score"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

// Learn evaluates a completed task and, if worthy, synthesises a skill.
// It is designed to be called asynchronously (fire-and-forget) so it
// never blocks the user-facing response.
func (l *Learner) Learn(ctx context.Context, req LearnRequest) error {
	ctx, span := trace.Tracer.Start(ctx, "learning.learn")
	span.SetAttributes(
		attribute.Int("learning.tools_used", len(req.ToolsUsed)),
		attribute.String("learning.goal_preview", toolwrap.TruncateForAudit(req.Goal, 80)),
	)
	defer span.End()

	logr := logger.GetLogger(ctx).With("fn", "learning.Learn")

	if l.skillRepo == nil {
		logr.Warn("skill repository not configured, skipping learning")
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_skipped",
			Metadata:  map[string]any{"reason": "skill_repo_nil"},
		})
		return nil
	}

	// Guard against empty input.
	if strings.TrimSpace(req.Goal) == "" || strings.TrimSpace(req.Output) == "" {
		logr.Debug("empty goal or output, skipping learning")
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_skipped",
			Metadata:  map[string]any{"reason": "empty_input"},
		})
		return nil
	}

	goalPreview := toolwrap.TruncateForAudit(req.Goal, 80)
	logr.Info("evaluating task for skill distillation",
		"goal_preview", goalPreview,
		"tools_used", len(req.ToolsUsed),
	)
	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventCommand,
		Actor:     "learner",
		Action:    "learning_started",
		Metadata: map[string]any{
			"goal_preview": goalPreview,
			"tools_used":   len(req.ToolsUsed),
		},
	})

	// Ask the LLM to decide if this task is worth distilling.
	prompt := buildDistillationPrompt(req)
	resp, err := l.expert.Do(ctx, expert.Request{
		Message:  prompt,
		TaskType: modelprovider.TaskEfficiency,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
			Silent:            true,
		},
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "llm_call_failed")
		logr.Warn("skill distillation LLM call failed", "error", err)
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_failed",
			Metadata: map[string]any{
				"reason":       "llm_call_error",
				"error":        err.Error(),
				"goal_preview": goalPreview,
			},
		})
		return fmt.Errorf("distillation LLM call: %w", err)
	}

	raw := llmutil.ExtractChoiceContent(resp.Choices)
	if raw == "" {
		logr.Warn("empty LLM response for skill distillation")
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_skipped",
			Metadata: map[string]any{
				"reason":       "empty_llm_response",
				"goal_preview": goalPreview,
			},
		})
		return nil
	}

	// Parse the JSON proposal from the LLM response.
	proposal, err := parseProposal(raw)
	if err != nil {
		logr.Warn("failed to parse skill proposal", "error", err, "raw", toolwrap.TruncateForAudit(raw, 200))
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_failed",
			Metadata: map[string]any{
				"reason":       "proposal_parse_error",
				"error":        err.Error(),
				"raw_preview":  toolwrap.TruncateForAudit(raw, 200),
				"goal_preview": goalPreview,
			},
		})
		return nil // non-fatal; best-effort
	}

	span.SetAttributes(
		attribute.Int("learning.novelty_score", proposal.NoveltyScore),
		attribute.Bool("learning.should_create", proposal.ShouldCreate),
		attribute.String("learning.proposed_name", proposal.Name),
	)

	if !proposal.ShouldCreate || proposal.NoveltyScore < minimumNoveltyScore {
		span.SetAttributes(attribute.String("learning.outcome", "below_novelty_threshold"))
		span.SetStatus(codes.Ok, "")
		logr.Info("task not novel enough for skill creation",
			"novelty_score", proposal.NoveltyScore,
			"should_create", proposal.ShouldCreate,
			"threshold", minimumNoveltyScore,
		)
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_skipped",
			Metadata: map[string]any{
				"reason":        "below_novelty_threshold",
				"novelty_score": proposal.NoveltyScore,
				"should_create": proposal.ShouldCreate,
				"threshold":     minimumNoveltyScore,
				"goal_preview":  goalPreview,
			},
		})
		return nil
	}

	// 1. Persist the skill to the filesystem-backed skill repository.
	addReq := skills.AddSkillRequest{
		Name:         proposal.Name,
		Description:  proposal.Description,
		Instructions: proposal.Instructions,
	}
	if err := l.skillRepo.Add(addReq); err != nil {
		// Skill may already exist — log and move on.
		logr.Warn("failed to add distilled skill", "name", proposal.Name, "error", err)
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_failed",
			Metadata: map[string]any{
				"reason":       "skill_add_error",
				"skill_name":   proposal.Name,
				"error":        err.Error(),
				"goal_preview": goalPreview,
			},
		})
		return nil
	}

	// 2. Index the skill in the vector store so the orchestrator can discover
	//    it via memory_search when solving similar future tasks.
	l.indexSkillInVectorStore(ctx, proposal)

	span.SetAttributes(attribute.String("learning.outcome", "skill_created"))
	span.SetStatus(codes.Ok, "")

	logr.Info("skill distilled and saved",
		"name", proposal.Name,
		"novelty_score", proposal.NoveltyScore,
	)

	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventCommand,
		Actor:     "learner",
		Action:    "skill_created",
		Metadata: map[string]any{
			"skill_name":    proposal.Name,
			"novelty_score": proposal.NoveltyScore,
			"description":   proposal.Description,
			"goal_preview":  toolwrap.TruncateForAudit(req.Goal, 120),
		},
	})

	return nil
}

// indexSkillInVectorStore upserts a skill summary into the vector store
// so the orchestrator can discover it via memory_search before starting
// work on new tasks.
func (l *Learner) indexSkillInVectorStore(ctx context.Context, proposal skillProposal) {
	if l.vectorStore == nil {
		return
	}

	ctx, span := trace.Tracer.Start(ctx, "learning.index_skill")
	span.SetAttributes(
		attribute.String("learning.skill_name", proposal.Name),
		attribute.String("learning.skill_description", proposal.Description),
	)
	defer span.End()

	logr := logger.GetLogger(ctx).With("fn", "learning.indexSkillInVectorStore")

	// Store a lightweight pointer — just enough for semantic search to
	// surface the skill. The orchestrator can then use load_skill to
	// fetch the full instructions when needed.
	content := fmt.Sprintf(
		"Learned skill available: %q — %s. Use load_skill(%q) to access the full workflow.",
		proposal.Name, proposal.Description, proposal.Name,
	)

	err := l.vectorStore.Upsert(ctx, vector.UpsertRequest{
		Items: []vector.BatchItem{
			{
				ID:   "skill:" + proposal.Name,
				Text: content,
				Metadata: map[string]string{
					"type":        skillMetadataType,
					"skill_name":  proposal.Name,
					"description": proposal.Description,
				},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "vector_index_failed")
		logr.Warn("failed to index skill in vector store", "name", proposal.Name, "error", err)
		return
	}
	span.SetStatus(codes.Ok, "")
}

// parseProposal extracts the JSON skill proposal from the LLM response.
// It handles responses where the JSON may be wrapped in markdown code fences.
func parseProposal(raw string) (skillProposal, error) {
	// Strip markdown code fences if present.
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		// Remove opening fence (```json or ```)
		if idx := strings.Index(cleaned, "\n"); idx >= 0 {
			cleaned = cleaned[idx+1:]
		}
		// Remove closing fence
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var p skillProposal
	if err := json.Unmarshal([]byte(cleaned), &p); err != nil {
		return skillProposal{}, fmt.Errorf("json unmarshal: %w", err)
	}
	return p, nil
}
