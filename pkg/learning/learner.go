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
//
// Design influences:
//   - SkillRL (2026): hierarchical skill bank, failure lessons as first-class data
//   - ExpeL: cross-task experiential learning with insight extraction
//   - Self-RAG: relevance-scored retrieval with reflection
package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	oteltrace "go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// defaultMinimumNoveltyScore is the threshold (1-10) above which a task is
// considered novel enough to warrant distillation into a skill.
const defaultMinimumNoveltyScore = 7

// skillMetadataType is the metadata type tag used when storing skills
// in the vector store. This allows memory_search to filter/distinguish
// learned skills from other memories.
const skillMetadataType = "learned_skill"

// semanticDedupMinSimilarity is the minimum similarity score for an
// existing skill to be considered a potential duplicate. Skills above
// this threshold are passed as context to the distillation LLM.
const semanticDedupMinSimilarity = 0.8

// Config tunes the post-session skill distillation pipeline.
type Config struct {
	// MinimumNoveltyScore is the LLM-assigned novelty score (1-10) above
	// which a completed task is considered novel enough to distill into a
	// reusable skill. Lower values create more skills; higher values are
	// more selective. Default is 7.
	MinimumNoveltyScore int `yaml:"minimum_novelty_score,omitempty" toml:"minimum_novelty_score,omitempty"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MinimumNoveltyScore: defaultMinimumNoveltyScore,
	}
}

// Learner reviews completed sessions and distills reusable skills.
type Learner struct {
	expert              expert.Expert
	skillRepo           skills.ISkillWriter
	vectorStore         vector.IStore
	auditor             audit.Auditor
	minimumNoveltyScore int
}

// NewLearner creates a Learner backed by the given expert, skill repository,
// vector store (for discoverability), and auditor. cfg.MinimumNoveltyScore
// controls novelty filtering; zero-value fields fall back to DefaultConfig.
func NewLearner(
	exp expert.Expert,
	skillRepo skills.ISkillWriter,
	vectorStore vector.IStore,
	auditor audit.Auditor,
	cfg Config,
) *Learner {
	if cfg.MinimumNoveltyScore <= 0 {
		cfg.MinimumNoveltyScore = defaultMinimumNoveltyScore
	}
	return &Learner{
		expert:              exp,
		skillRepo:           skillRepo,
		vectorStore:         vectorStore,
		auditor:             auditor,
		minimumNoveltyScore: cfg.MinimumNoveltyScore,
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

	// ToolTrace is a compressed summary of tool calls and their outcomes.
	// Unlike ToolsUsed (which only has names), this captures what each tool
	// returned, enabling the distillation LLM to accurately describe what
	// worked and what didn't. Inspired by SkillRL's failure lesson extraction.
	ToolTrace string
}

// skillProposal is the JSON structure returned by the LLM when it
// decides that a task should be distilled into a skill.
type skillProposal struct {
	ShouldCreate   bool   `json:"should_create"`
	NoveltyScore   int    `json:"novelty_score"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Instructions   string `json:"instructions"`
	UpdateExisting string `json:"update_existing"`
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

	if skip := l.validateRequest(ctx, req); skip {
		return nil
	}

	goalPreview := toolwrap.TruncateForAudit(req.Goal, 80)
	l.auditLearningStarted(ctx, goalPreview, len(req.ToolsUsed))

	// Semantic dedup: search for existing skills similar to this goal
	// before sending to the LLM. This allows the LLM to decide whether
	// to update an existing skill rather than creating a duplicate.
	existingSkills := l.findSimilarSkills(ctx, req.Goal)

	proposal, err := l.distill(ctx, req, goalPreview, existingSkills)
	if err != nil {
		return err
	}
	if proposal == nil {
		return nil // skipped (low novelty, parse error, empty response)
	}

	span.SetAttributes(
		attribute.Int("learning.novelty_score", proposal.NoveltyScore),
		attribute.Bool("learning.should_create", proposal.ShouldCreate),
		attribute.String("learning.proposed_name", proposal.Name),
		attribute.String("learning.update_existing", proposal.UpdateExisting),
	)

	if !l.isNovelEnough(*proposal) {
		l.auditBelowThreshold(ctx, *proposal, goalPreview)
		return nil
	}

	return l.persistSkill(ctx, *proposal, req.Goal)
}

// ---------------------------------------------------------------------------
// Private helpers — validation & guards
// ---------------------------------------------------------------------------

// validateRequest checks preconditions (nil repo, empty input) and returns
// true when learning should be skipped.
func (l *Learner) validateRequest(ctx context.Context, req LearnRequest) bool {
	logr := logger.GetLogger(ctx).With("fn", "learning.Learn")

	if l.skillRepo == nil {
		logr.Warn("skill repository not configured, skipping learning")
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_skipped",
			Metadata:  map[string]any{"reason": "skill_repo_nil"},
		})
		return true
	}

	if strings.TrimSpace(req.Goal) == "" || strings.TrimSpace(req.Output) == "" {
		logr.Debug("empty goal or output, skipping learning")
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_skipped",
			Metadata:  map[string]any{"reason": "empty_input"},
		})
		return true
	}

	return false
}

// isNovelEnough returns true when the proposal passes the novelty gate.
func (l *Learner) isNovelEnough(p skillProposal) bool {
	return p.ShouldCreate && p.NoveltyScore >= l.minimumNoveltyScore
}

// ---------------------------------------------------------------------------
// Private helpers — semantic dedup (inspired by ExpeL cross-task learning)
// ---------------------------------------------------------------------------

// findSimilarSkills searches the vector store for existing learned skills
// that are semantically similar to the current goal. Returns a formatted
// string describing these skills for injection into the distillation prompt,
// enabling the LLM to decide update-vs-create. Returns "" if no store or
// no similar skills.
func (l *Learner) findSimilarSkills(ctx context.Context, goal string) string {
	if l.vectorStore == nil {
		return ""
	}

	results, err := l.vectorStore.Search(ctx, vector.SearchRequest{
		Query:  goal,
		Limit:  3,
		Filter: map[string]string{"type": skillMetadataType},
	})
	if err != nil {
		logger.GetLogger(ctx).Debug("semantic dedup search failed", "error", err)
		return ""
	}

	var sb strings.Builder
	for _, r := range results {
		if r.Score < semanticDedupMinSimilarity {
			continue
		}
		name := r.Metadata["skill_name"]
		desc := r.Metadata["description"]
		if name == "" {
			continue
		}
		fmt.Fprintf(&sb, "- **%s** (similarity: %.2f): %s\n", name, r.Score, desc)
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Private helpers — LLM distillation
// ---------------------------------------------------------------------------

// distill sends the task to the LLM for novelty evaluation and returns
// the parsed proposal. Returns (nil, nil) when the task should be skipped
// (empty response, parse failure). Returns (nil, err) on fatal errors.
func (l *Learner) distill(ctx context.Context, req LearnRequest, goalPreview, existingSkills string) (*skillProposal, error) {
	raw, err := l.callDistillationLLM(ctx, req, goalPreview, existingSkills)
	if err != nil {
		return nil, err
	}

	if raw == "" {
		logr := logger.GetLogger(ctx).With("fn", "learning.distill")
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
		return nil, nil
	}

	proposal, err := parseProposal(raw)
	if err != nil {
		// Retry once with a stricter JSON-only prompt (fixes quote/parse issue).
		logr := logger.GetLogger(ctx).With("fn", "learning.distill")
		logr.Info("first parse failed, retrying with stricter prompt", "error", err)

		retryRaw, retryErr := l.callRetryLLM(ctx, req, goalPreview)
		if retryErr != nil {
			logr.Warn("retry LLM call also failed", "error", retryErr)
			l.auditParseFailure(ctx, err, raw, goalPreview)
			return nil, nil
		}

		proposal, err = parseProposal(retryRaw)
		if err != nil {
			logr.Warn("retry parse also failed", "error", err)
			l.auditParseFailure(ctx, err, retryRaw, goalPreview)
			return nil, nil
		}

		logr.Info("retry succeeded, skill proposal parsed")
	}

	return &proposal, nil
}

// callDistillationLLM invokes the expert with the distillation prompt
// and returns the raw response text.
func (l *Learner) callDistillationLLM(ctx context.Context, req LearnRequest, goalPreview, existingSkills string) (string, error) {
	logr := logger.GetLogger(ctx).With("fn", "learning.callDistillationLLM")
	span := oteltrace.SpanFromContext(ctx)

	prompt := buildDistillationPrompt(req, existingSkills)
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
		return "", fmt.Errorf("distillation LLM call: %w", err)
	}

	return llmutil.ExtractChoiceContent(resp.Choices), nil
}

// callRetryLLM invokes the expert with a strict JSON-only re-prompt
// when the initial distillation response fails to parse. The prompt
// includes a summarized version of the original task because expert.Do()
// calls are stateless — without context, the LLM wouldn't know what
// to evaluate.
func (l *Learner) callRetryLLM(ctx context.Context, req LearnRequest, goalPreview string) (string, error) {
	logr := logger.GetLogger(ctx).With("fn", "learning.callRetryLLM")

	resp, err := l.expert.Do(ctx, expert.Request{
		Message:  buildRetryPrompt(req),
		TaskType: modelprovider.TaskEfficiency,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
			Silent:            true,
		},
	})
	if err != nil {
		logr.Warn("retry LLM call failed", "error", err, "goal_preview", goalPreview)
		return "", fmt.Errorf("retry LLM call: %w", err)
	}

	return llmutil.ExtractChoiceContent(resp.Choices), nil
}

// auditParseFailure records a parse failure in the audit log.
func (l *Learner) auditParseFailure(ctx context.Context, parseErr error, raw, goalPreview string) {
	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventCommand,
		Actor:     "learner",
		Action:    "learning_failed",
		Metadata: map[string]any{
			"reason":       "proposal_parse_error",
			"error":        parseErr.Error(),
			"raw_preview":  toolwrap.TruncateForAudit(raw, 200),
			"goal_preview": goalPreview,
		},
	})
}

// ---------------------------------------------------------------------------
// Private helpers — skill persistence
// ---------------------------------------------------------------------------

// persistSkill writes the proposal to the skill repository and vector
// store, then records the audit trail. If the proposal specifies
// UpdateExisting, the existing skill is updated instead of creating
// a new one. This implements the ExpeL-inspired update-or-create pattern.
func (l *Learner) persistSkill(ctx context.Context, proposal skillProposal, goal string) error {
	logr := logger.GetLogger(ctx).With("fn", "learning.persistSkill")
	span := oteltrace.SpanFromContext(ctx)

	now := time.Now().UTC().Format(time.RFC3339)

	// If updating an existing skill, use Update() path.
	if proposal.UpdateExisting != "" && l.skillRepo.Exists(proposal.UpdateExisting) {
		return l.updateExistingSkill(ctx, proposal, goal, now)
	}

	// Enrich instructions with metadata timestamps.
	enrichedInstructions := fmt.Sprintf("<!-- created_at: %s -->\n<!-- updated_at: %s -->\n%s",
		now, now, proposal.Instructions)

	addReq := skills.AddSkillRequest{
		Name:         proposal.Name,
		Description:  proposal.Description,
		Instructions: enrichedInstructions,
	}
	if err := l.skillRepo.Add(addReq); err != nil {
		logr.Warn("failed to add distilled skill", "name", proposal.Name, "error", err)
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_failed",
			Metadata: map[string]any{
				"reason":       "skill_add_error",
				"skill_name":   proposal.Name,
				"error":        err.Error(),
				"goal_preview": toolwrap.TruncateForAudit(goal, 80),
			},
		})
		return nil
	}

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
			"goal_preview":  toolwrap.TruncateForAudit(goal, 120),
		},
	})

	return nil
}

// updateExistingSkill updates an existing skill with new instructions
// from the latest execution. This prevents the "skill already exists"
// error and allows skills to evolve as the agent learns more.
func (l *Learner) updateExistingSkill(ctx context.Context, proposal skillProposal, goal, now string) error {
	logr := logger.GetLogger(ctx).With("fn", "learning.updateExistingSkill")
	span := oteltrace.SpanFromContext(ctx)

	// Preserve the skill name from the existing skill.
	skillName := proposal.UpdateExisting

	enrichedInstructions := fmt.Sprintf("<!-- updated_at: %s -->\n%s", now, proposal.Instructions)

	updateReq := skills.AddSkillRequest{
		Name:         skillName,
		Description:  proposal.Description,
		Instructions: enrichedInstructions,
	}
	if err := l.skillRepo.Update(updateReq); err != nil {
		logr.Warn("failed to update existing skill", "name", skillName, "error", err)
		l.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventCommand,
			Actor:     "learner",
			Action:    "learning_failed",
			Metadata: map[string]any{
				"reason":       "skill_update_error",
				"skill_name":   skillName,
				"error":        err.Error(),
				"goal_preview": toolwrap.TruncateForAudit(goal, 80),
			},
		})
		return nil
	}

	// Re-index in vector store with updated content.
	l.indexSkillInVectorStore(ctx, skillProposal{
		Name:        skillName,
		Description: proposal.Description,
	})

	span.SetAttributes(attribute.String("learning.outcome", "skill_updated"))
	span.SetStatus(codes.Ok, "")

	logr.Info("existing skill updated",
		"name", skillName,
		"novelty_score", proposal.NoveltyScore,
	)

	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventCommand,
		Actor:     "learner",
		Action:    "skill_updated",
		Metadata: map[string]any{
			"skill_name":    skillName,
			"novelty_score": proposal.NoveltyScore,
			"description":   proposal.Description,
			"goal_preview":  toolwrap.TruncateForAudit(goal, 120),
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

	// Audit event for vector store indexing (Fix 5: observability).
	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "learner",
		Action:    "skill_indexed_in_vector_store",
		Metadata: map[string]any{
			"skill_name":  proposal.Name,
			"description": proposal.Description,
		},
	})
}

// ---------------------------------------------------------------------------
// Private helpers — audit events
// ---------------------------------------------------------------------------

// auditLearningStarted records the start of a learning attempt.
func (l *Learner) auditLearningStarted(ctx context.Context, goalPreview string, toolCount int) {
	logr := logger.GetLogger(ctx).With("fn", "learning.Learn")
	logr.Info("evaluating task for skill distillation",
		"goal_preview", goalPreview,
		"tools_used", toolCount,
	)
	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventCommand,
		Actor:     "learner",
		Action:    "learning_started",
		Metadata: map[string]any{
			"goal_preview": goalPreview,
			"tools_used":   toolCount,
		},
	})
}

// auditBelowThreshold records that a task was skipped because it did
// not meet the novelty threshold.
func (l *Learner) auditBelowThreshold(ctx context.Context, proposal skillProposal, goalPreview string) {
	logr := logger.GetLogger(ctx).With("fn", "learning.Learn")
	span := oteltrace.SpanFromContext(ctx)

	span.SetAttributes(attribute.String("learning.outcome", "below_novelty_threshold"))
	span.SetStatus(codes.Ok, "")
	logr.Info("task not novel enough for skill creation",
		"novelty_score", proposal.NoveltyScore,
		"should_create", proposal.ShouldCreate,
		"threshold", l.minimumNoveltyScore,
	)
	l.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventCommand,
		Actor:     "learner",
		Action:    "learning_skipped",
		Metadata: map[string]any{
			"reason":        "below_novelty_threshold",
			"novelty_score": proposal.NoveltyScore,
			"should_create": proposal.ShouldCreate,
			"threshold":     l.minimumNoveltyScore,
			"goal_preview":  goalPreview,
		},
	})
}

// ---------------------------------------------------------------------------
// Private helpers — JSON parsing
// ---------------------------------------------------------------------------

// parseProposal extracts the JSON skill proposal from the LLM response.
// It handles responses where the JSON may be wrapped in markdown code fences
// or buried inside verbose prose by searching for the first complete JSON
// object via brace-matching.
func parseProposal(raw string) (skillProposal, error) {
	cleaned := strings.TrimSpace(raw)

	// Fast path: direct JSON.
	var p skillProposal
	if err := json.Unmarshal([]byte(cleaned), &p); err == nil {
		return p, nil
	}

	// Strip markdown code fences if present.
	if strings.HasPrefix(cleaned, "```") {
		if idx := strings.Index(cleaned, "\n"); idx >= 0 {
			cleaned = cleaned[idx+1:]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
		if err := json.Unmarshal([]byte(cleaned), &p); err == nil {
			return p, nil
		}
	}

	// Last resort: find the first top-level JSON object via brace-matching.
	if extracted, ok := extractFirstJSON(raw); ok {
		if err := json.Unmarshal([]byte(extracted), &p); err == nil {
			return p, nil
		}
	}

	return skillProposal{}, fmt.Errorf("json unmarshal: no valid JSON object found in response")
}

// extractFirstJSON finds the first complete JSON object in a string by
// matching braces. This handles the case where the LLM wraps JSON in
// verbose markdown prose.
func extractFirstJSON(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}

	depth := 0
	inStr := false
	escaped := false

	for i := start; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inStr {
			escaped = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == '{' {
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}

	return "", false
}
