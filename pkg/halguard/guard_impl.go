package halguard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// verificationModel pairs a model with its provider key for deduplication.
type verificationModel struct {
	key   string // e.g. "anthropic/claude-sonnet-4-6"
	model model.Model
}

// guard is the concrete implementation of Guard backed by a ModelProvider.
type guard struct {
	modelProvider modelprovider.ModelProvider
	config        Config
}

// New creates a Guard with the given model provider and options.
// The model provider is used to collect diverse models for cross-model
// consistency checking. When options are not provided, sensible defaults
// are used (pre-check enabled, post-check enabled, 3 cross-model samples).
func New(
	modelProvider modelprovider.ModelProvider,
	opts ...Option,
) Guard {
	cfg := Config{
		EnablePreCheck:  true,
		EnablePostCheck: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg = cfg.defaults()

	return &guard{
		modelProvider: modelProvider,
		config:        cfg,
	}
}

// PreCheck scores the sub-agent goal for fabrication risk.
// Returns a PreCheckResult with a confidence score between 0.0 and 1.0.
// Uses multi-signal weighted analysis: structural indicators, regex-based
// pattern detection, information density, and temporal urgency signals.
// The caller decides whether to proceed based on the score.
func (g *guard) PreCheck(ctx context.Context, req PreCheckRequest) (PreCheckResult, error) {
	if !g.config.EnablePreCheck {
		return PreCheckResult{
			Confidence: 1.0,
			Summary:    "pre-check disabled",
		}, nil
	}

	logr := logger.GetLogger(ctx).With("fn", "halguard.PreCheck")

	// Score the goal using multi-signal analysis.
	_, signals := scoreGoal(req.Goal)

	// Also check the context field if provided.
	// Context penalties are weighted at 70% since context is supplementary.
	if req.Context != "" {
		_, ctxSignals := scoreGoal(req.Context)
		signals = signals.MergeScaled(ctxSignals, 0.7)
	}

	penalty := signals.Penalty()
	confidence := 1.0 - penalty
	if confidence < 0 {
		confidence = 0
	}

	// Build summary.
	var summary string
	if confidence >= 0.8 {
		summary = "goal appears genuine"
	} else if confidence >= 0.5 {
		summary = "goal has moderate fabrication signals"
	} else {
		summary = "goal has strong fabrication signals"
	}
	if signals.HasAny() {
		summary += ": " + signals.String()
	}

	logr.Info("grounding score computed",
		"confidence", confidence,
		"penalty", penalty,
		"signals", signals.String())

	return PreCheckResult{
		Confidence: confidence,
		Signals:    signals,
		Summary:    summary,
	}, nil
}

// PostCheck verifies sub-agent output after execution.
// It selects a verification tier based on output characteristics and
// applies the appropriate level of cross-model consistency checking.
func (g *guard) PostCheck(ctx context.Context, req PostCheckRequest) (VerificationResult, error) {
	if !g.config.EnablePostCheck {
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			Tier:          TierNone,
		}, nil
	}

	logr := logger.GetLogger(ctx).With("fn", "halguard.PostCheck",
		"output_len", len(req.Output), "tool_calls", req.ToolCallsMade)

	tier := g.selectTier(req)
	logr.Info("verification tier selected", "tier", tier)

	switch tier {
	case TierNone:
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			Tier:          TierNone,
		}, nil
	case TierLight:
		return g.verifyLight(ctx, req)
	case TierFull:
		return g.verifyFull(ctx, req)
	}

	return VerificationResult{
		IsFactual:     true,
		CorrectedText: req.Output,
		Tier:          TierNone,
	}, nil
}

// selectTier determines which verification tier to apply based on output
// characteristics. Outputs that are short and backed by tool results
// skip verification entirely; longer or ungrounded outputs get increasingly
// thorough checks.
func (g *guard) selectTier(req PostCheckRequest) VerifyTier {
	outputLen := len(req.Output)

	// Short, tool-grounded outputs are likely factual.
	if outputLen < g.config.LightThresholdChars && req.ToolCallsMade > 0 {
		return TierNone
	}

	// Check for fabrication signals in the output itself.
	outputPenalty, _ := scoreGoal(req.Output)
	if outputPenalty > 0.3 {
		return TierFull
	}

	// No tool calls at all is suspicious — the output is pure generation.
	if req.ToolCallsMade == 0 && outputLen > g.config.LightThresholdChars {
		return TierFull
	}

	// Long outputs get full verification.
	if outputLen >= g.config.FullThresholdChars {
		return TierFull
	}

	// Medium-length outputs get light verification.
	if outputLen >= g.config.LightThresholdChars {
		return TierLight
	}

	return TierNone
}

// verifyLight performs a single-model sanity check on the output.
// It uses one efficiency model to evaluate whether the output appears
// fabricated or contains hallucinated content.
func (g *guard) verifyLight(ctx context.Context, req PostCheckRequest) (VerificationResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "halguard.verifyLight")

	models, err := g.collectVerificationModels(ctx, 1)
	if err != nil {
		logr.Warn("failed to collect verification models, skipping", "error", err)
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			Tier:          TierLight,
		}, nil
	}

	goalText := req.Goal
	if req.Context != "" {
		goalText = fmt.Sprintf("Context:\n%s\n\nGoal:\n%s", req.Context, req.Goal)
	}

	prompt := fmt.Sprintf(`You are a factual consistency checker. Analyze the following sub-agent output and determine if it contains hallucinated or fabricated content.

ORIGINAL GOAL: %s

SUB-AGENT OUTPUT:
%s

Check for:
1. Does the output contain fabricated data (invented metrics, fake incidents, role-play scenarios)?
2. Does the output make specific claims that could not have been verified by the tools available?
3. Does the output describe a hypothetical scenario as if it were real?

Respond with a JSON object:
{"is_factual": true/false, "reason": "brief explanation"}

Output ONLY the JSON, no other text.`, goalText, req.Output)

	result, genErr := generateText(ctx, models[0].model, prompt, 500)
	if genErr != nil {
		logr.Warn("light verification generation failed", "error", genErr)
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			Tier:          TierLight,
		}, nil
	}

	var verdict struct {
		IsFactual bool   `json:"is_factual"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(extractJSON(result)), &verdict); err != nil {
		logr.Warn("failed to parse light verification result", "error", err, "raw", result)
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			Tier:          TierLight,
		}, nil
	}

	logr.Info("light verification complete", "is_factual", verdict.IsFactual, "reason", verdict.Reason)

	return VerificationResult{
		IsFactual:     verdict.IsFactual,
		CorrectedText: req.Output, // Light tier does not correct — only detects.
		BlockScores: []BlockScore{{
			Text:   req.Output,
			Label:  labelFromBool(verdict.IsFactual),
			Reason: verdict.Reason,
		}},
		Tier: TierLight,
	}, nil
}

// verifyFull implements the full Finch-Zk cross-model verification pipeline:
// 1. Collect diverse models (efficiency-first, then other task types)
// 2. Generate cross-model samples in parallel
// 3. Segment output into blocks
// 4. Batch-judge each block against all samples
// 5. Apply targeted corrections to contradicted blocks using a different model
func (g *guard) verifyFull(ctx context.Context, req PostCheckRequest) (VerificationResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "halguard.verifyFull")

	// Stage 1: Collect diverse verification models.
	models, err := g.collectVerificationModels(ctx, g.config.CrossModelSamples)
	if err != nil {
		logr.Warn("failed to collect verification models, falling back to light", "error", err)
		return g.verifyLight(ctx, req)
	}
	logr.Info("collected verification models", "count", len(models),
		"models", modelKeys(models))

	// Stage 2: Generate cross-model samples in parallel.
	goalText := req.Goal
	if req.Context != "" {
		goalText = fmt.Sprintf("Context:\n%s\n\nGoal:\n%s", req.Context, req.Goal)
	}
	samples := g.generateCrossModelSamples(ctx, models, goalText)
	if len(samples) == 0 {
		logr.Warn("no cross-model samples generated, falling back to light")
		return g.verifyLight(ctx, req)
	}
	logr.Info("generated cross-model samples", "count", len(samples))

	// Stage 3: Segment output into blocks.
	blocks := segmentIntoBlocks(req.Output)
	if len(blocks) > g.config.MaxBlocksToJudge {
		blocks = blocks[:g.config.MaxBlocksToJudge]
	}
	logr.Info("segmented output into blocks", "count", len(blocks))

	// Stage 4: Batch-judge blocks against samples.
	scores, judgeErr := g.batchJudge(ctx, models[0].model, blocks, samples)
	if judgeErr != nil {
		logr.Warn("batch judging failed", "error", judgeErr)
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			Tier:          TierFull,
		}, nil
	}

	// Check for contradictions.
	hasContradictions := false
	for _, s := range scores {
		if s.Label == BlockContradiction {
			hasContradictions = true
			break
		}
	}

	if !hasContradictions {
		logr.Info("full verification: no contradictions found")
		return VerificationResult{
			IsFactual:     true,
			CorrectedText: req.Output,
			BlockScores:   scores,
			Tier:          TierFull,
		}, nil
	}

	// Stage 5: Targeted block correction using a different model.
	correctionModel := g.selectCorrectionModel(models, req.GenerationModel)
	correctedText := g.correctBlocks(ctx, correctionModel, req.Output, blocks, scores, samples)

	logr.Info("full verification: contradictions found and corrected",
		"contradictions", scores.countContradictions())

	return VerificationResult{
		IsFactual:     false,
		CorrectedText: correctedText,
		BlockScores:   scores,
		Tier:          TierFull,
	}, nil
}

// collectVerificationModels gathers diverse models for cross-model checking.
// Strategy: efficiency models first, then other task types for diversity.
func (g *guard) collectVerificationModels(ctx context.Context, needed int) ([]verificationModel, error) {
	logr := logger.GetLogger(ctx).With("fn", "halguard.collectVerificationModels")

	var collected []verificationModel
	seen := map[string]bool{}

	// Priority order: efficiency (fast/cheap), then others for diversity.
	taskTypes := []modelprovider.TaskType{
		modelprovider.TaskEfficiency,
		modelprovider.TaskToolCalling,
		modelprovider.TaskPlanning,
		modelprovider.TaskGeneralTask,
		modelprovider.TaskScientificReasoning,
	}

	for _, taskType := range taskTypes {
		if len(collected) >= needed {
			break
		}

		modelMap, err := g.modelProvider.GetModel(ctx, taskType)
		if err != nil {
			logr.Debug("no models for task type", "task_type", taskType, "error", err)
			continue
		}

		for key, m := range modelMap {
			if seen[key] {
				continue
			}
			seen[key] = true
			collected = append(collected, verificationModel{key: key, model: m})
			if len(collected) >= needed {
				break
			}
		}
	}

	if len(collected) == 0 {
		return nil, fmt.Errorf("no verification models available from any task type")
	}

	logr.Info("verification models collected",
		"requested", needed,
		"actual", len(collected),
		"models", modelKeys(collected))

	return collected, nil
}

// generateCrossModelSamples generates independent responses to the same
// goal from different models in parallel.
func (g *guard) generateCrossModelSamples(ctx context.Context, models []verificationModel, goal string) []string {
	logr := logger.GetLogger(ctx).With("fn", "halguard.generateCrossModelSamples")

	prompt := fmt.Sprintf(`Answer the following question or complete the following task concisely and factually.
If you don't know the answer, say "I don't know" — do not fabricate information.

Task: %s`, goal)

	var mu sync.Mutex
	var samples []string
	errGroup, _ := errgroup.WithContext(ctx)
	for _, vm := range models {
		errGroup.Go(func() error {
			sample, err := generateText(ctx, vm.model, prompt, 2000)
			if err != nil {
				logr.Warn("cross-model sample generation failed",
					"model", vm.key, "error", err)
				return nil
			}
			if strings.TrimSpace(sample) == "" {
				return nil
			}
			mu.Lock()
			samples = append(samples, sample)
			mu.Unlock()
			return nil
		})
	}
	_ = errGroup.Wait()

	return samples
}

// batchJudge evaluates all blocks against all samples in a single LLM call.
func (g *guard) batchJudge(ctx context.Context, judge model.Model, blocks []string, samples []string) (BlockScores, error) {
	var blockList strings.Builder
	for i, b := range blocks {
		fmt.Fprintf(&blockList, "[%d] %s\n", i+1, b)
	}

	var sampleList strings.Builder
	labels := []string{"A", "B", "C", "D", "E", "F"}
	for i, s := range samples {
		label := labels[i%len(labels)]
		if len(s) > 1500 {
			s = s[:1500] + "..."
		}
		fmt.Fprintf(&sampleList, "[%s] %s\n\n", label, s)
	}

	prompt := fmt.Sprintf(`You are a factual consistency judge. Compare each numbered block from the TARGET response against the REFERENCE responses generated by independent models for the same task.

For each block, assign a label:
- ACCURATE: The block's claims are consistent with or supported by the references.
- CONTRADICTION: The block makes specific factual claims that directly contradict one or more references.
- NEUTRAL: Insufficient information to determine accuracy (e.g. subjective, stylistic, or generic content).

IMPORTANT: Only label as CONTRADICTION when there is a clear factual inconsistency, not just a difference in phrasing or emphasis.

TARGET BLOCKS:
%s

REFERENCE RESPONSES:
%s

Respond with a JSON array, one object per block:
[{"block": 1, "label": "ACCURATE", "reason": "..."}, ...]

Output ONLY the JSON array.`, blockList.String(), sampleList.String())

	result, err := generateText(ctx, judge, prompt, 4000)
	if err != nil {
		return nil, fmt.Errorf("batch judge generation failed: %w", err)
	}

	var judgments []struct {
		Block  int    `json:"block"`
		Label  string `json:"label"`
		Reason string `json:"reason"`
	}
	if parseErr := json.Unmarshal([]byte(extractJSON(result)), &judgments); parseErr != nil {
		return nil, fmt.Errorf("failed to parse judge response: %w (raw: %s)", parseErr, result)
	}

	scores := make(BlockScores, len(blocks))
	for i, b := range blocks {
		scores[i] = BlockScore{
			Text:  b,
			Label: BlockNeutral,
		}
	}
	for _, j := range judgments {
		idx := j.Block - 1
		if idx < 0 || idx >= len(scores) {
			continue
		}
		scores[idx].Label = toBlockLabel(j.Label)
		scores[idx].Reason = j.Reason
	}

	return scores, nil
}

// selectCorrectionModel picks a model from a different family than the
// generation model for cross-model correction.
func (g *guard) selectCorrectionModel(available []verificationModel, generationModelMap modelprovider.ModelMap) model.Model {
	// Extract the generation model key from the map.
	var genKey string
	for k := range generationModelMap {
		genKey = k
		break
	}
	genLower := strings.ToLower(genKey)

	for _, vm := range available {
		keyLower := strings.ToLower(vm.key)
		if strings.Contains(genLower, "claude") && !strings.Contains(keyLower, "anthropic") {
			return vm.model
		}
		if strings.Contains(genLower, "gpt") && !strings.Contains(keyLower, "openai") {
			return vm.model
		}
		if strings.Contains(genLower, "gemini") && !strings.Contains(keyLower, "gemini") {
			return vm.model
		}
	}

	return available[0].model
}

// correctBlocks applies targeted corrections only to CONTRADICTION blocks.
func (g *guard) correctBlocks(ctx context.Context, corrector model.Model, originalText string, blocks []string, scores []BlockScore, samples []string) string {
	logr := logger.GetLogger(ctx).With("fn", "halguard.correctBlocks")

	result := originalText
	for i, score := range scores {
		if i >= len(blocks) {
			break
		}

		if score.Label != BlockContradiction {
			continue
		}

		var sampleList strings.Builder
		labels := []string{"A", "B", "C", "D", "E", "F"}
		for j, s := range samples {
			label := labels[j%len(labels)]
			if len(s) > 1000 {
				s = s[:1000] + "..."
			}
			fmt.Fprintf(&sampleList, "[%s] %s\n\n", label, s)
		}
		sampleEvidence := strings.TrimSpace(sampleList.String())

		prompt := fmt.Sprintf(`Fix the factual error in the following text block. Preserve the style and intent — only correct the factual claim.

ORIGINAL BLOCK: %s

DETECTED ERROR: %s

REFERENCE EVIDENCE: %s

Write ONLY the corrected version of this block. No explanations.`, blocks[i], score.Reason, sampleEvidence)

		fixed, err := generateText(ctx, corrector, prompt, 1000)
		if err != nil {
			logr.Warn("block correction failed, keeping original", "block", i, "error", err)
			continue
		}

		fixed = strings.TrimSpace(fixed)
		if fixed == "" {
			continue
		}
		result = strings.Replace(result, blocks[i], fixed, 1)
	}

	return result
}

// --- Helpers ---

// generateText performs a one-shot text generation with the given model.
// An OTel span is created so the call appears in Langfuse traces — without
// this, halguard model calls are invisible because we call
// model.GenerateContent directly (bypassing the trpc-agent-go runner).
func generateText(ctx context.Context, m model.Model, prompt string, maxTokens int) (string, error) {
	// Create a child span following the same naming convention as trpc-agent-go:
	// "chat {model_name}" so Langfuse recognises it as an LLM generation.
	modelName := ""
	if info := m.Info(); info.Name != "" {
		modelName = info.Name
	}
	spanName := "chat"
	if modelName != "" {
		spanName = fmt.Sprintf("chat %s", modelName)
	}
	ctx, span := trace.Tracer.Start(ctx, spanName)
	span.SetAttributes(
		attribute.String("gen_ai.system", "trpc.go.agent"),
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.request.model", modelName),
		attribute.String("halguard.caller", "generateText"),
	)
	defer span.End()

	req := &model.Request{
		Messages: []model.Message{model.NewUserMessage(prompt)},
		GenerationConfig: model.GenerationConfig{
			Stream:    true,
			MaxTokens: model.IntPtr(maxTokens),
		},
	}

	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		span.SetAttributes(attribute.String("error.type", "generate_content_failed"))
		return "", fmt.Errorf("generate content: %w", err)
	}

	var sb strings.Builder
	var totalInputTokens, totalOutputTokens int
	var responseModel string
	for resp := range ch {
		if resp.Error != nil {
			if sb.Len() > 0 {
				break
			}
			span.SetAttributes(attribute.String("error.type", "generation_error"))
			return "", fmt.Errorf("generation error: %s", resp.Error.Message)
		}
		if resp.Model != "" {
			responseModel = resp.Model
		}
		if resp.Usage != nil {
			totalInputTokens += resp.Usage.PromptTokens
			totalOutputTokens += resp.Usage.CompletionTokens
		}
		for _, c := range resp.Choices {
			if c.Message.Content != "" {
				sb.WriteString(c.Message.Content)
			}
		}
	}

	// Record usage metrics on the span so they appear in Langfuse.
	if responseModel != "" {
		span.SetAttributes(attribute.String("gen_ai.response.model", responseModel))
	}
	if totalInputTokens > 0 || totalOutputTokens > 0 {
		span.SetAttributes(
			attribute.Int("gen_ai.usage.input_tokens", totalInputTokens),
			attribute.Int("gen_ai.usage.output_tokens", totalOutputTokens),
		)
	}

	return sb.String(), nil
}

// extractJSON attempts to find a JSON object or array in the given text.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")

	start := -1
	switch {
	case startObj >= 0 && startArr >= 0:
		start = min(startObj, startArr)
	case startObj >= 0:
		start = startObj
	case startArr >= 0:
		start = startArr
	}
	if start < 0 {
		return s
	}

	end := strings.LastIndexAny(s, "}]")
	if end < start {
		return s[start:]
	}
	return s[start : end+1]
}

func modelKeys(models []verificationModel) []string {
	keys := make([]string, len(models))
	for i, m := range models {
		keys[i] = m.key
	}
	return keys
}

func labelFromBool(factual bool) BlockLabel {
	if factual {
		return BlockAccurate
	}
	return BlockContradiction
}

func toBlockLabel(s string) BlockLabel {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ACCURATE":
		return BlockAccurate
	case "CONTRADICTION":
		return BlockContradiction
	default:
		return BlockNeutral
	}
}
