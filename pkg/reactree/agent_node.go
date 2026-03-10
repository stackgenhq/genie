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
	"time"

	"github.com/stackgenhq/genie/pkg/clarify"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/retrier"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// AgentNodeConfig holds configuration for creating an agent node function.
type AgentNodeConfig struct {
	Goal          string
	Expert        expert.Expert
	WorkingMemory *memory.WorkingMemory
	Episodic      memory.EpisodicMemory
	MaxDecisions  int
	Tools         []tool.Tool
	// TaskType selects the model for this node via ModelProvider.GetModel().
	// If empty, defaults to TaskPlanning.
	TaskType modelprovider.TaskType

	// Attachments are file/media attachments from the incoming message.
	// Image attachments are passed as multimodal content to the LLM.
	Attachments []messenger.Attachment

	// BudgetExhaustedTools lists tool names whose budget has been reached.
	// The adaptive loop sets this when tools in ToolBudgets have hit their
	// limits. A prompt hint is injected telling the LLM these tools are
	// unavailable; the tools themselves are also stripped from the list.
	BudgetExhaustedTools []string

	// SystemInstruction, when set, makes this node use a lightweight
	// llmagent instead of the full Expert. This prevents plan-step
	// sub-agents from inheriting the main agent's persona (which
	// contains orchestration patterns and causes tool hallucination).
	SystemInstruction string

	// ModelProvider is used to resolve the model when SystemInstruction
	// is set (lightweight mode). Ignored when using Expert.
	ModelProvider modelprovider.ModelProvider

	// Hooks provides access to the execution lifecycle events
	Hooks hooks.ExecutionHook

	// FailureReflector generates verbal reflections on agent failures.
	// When set, failed episodes are stored with a reflection explaining
	// what went wrong and what to try differently. When nil, failures
	// are stored with just the raw error output.
	FailureReflector memory.FailureReflector

	// ImportanceScorer assigns 1-10 importance scores to episodes.
	// When set, the score is stored on the episode and influences
	// weighted retrieval. When nil, episodes use a neutral default.
	ImportanceScorer memory.ImportanceScorer

	// WisdomStore provides access to consolidated daily wisdom notes.
	// When set, recent wisdom notes are injected into the agent prompt
	// so the agent benefits from distilled past experiences.
	WisdomStore memory.WisdomStore
}

// NewAgentNodeFunc creates a graph.NodeFunc that wraps an expert.Expert call.
// The returned function reads the goal from state, enriches the prompt with
// working memory and episodic memory, calls the expert, and writes the result
// back to state. This is the bridge between the ReAcTree concept of an
// "agent node" and trpc-agent-go's graph execution model.
func NewAgentNodeFunc(cfg AgentNodeConfig) graph.NodeFunc {
	ep := cfg.Episodic
	if ep == nil {
		ep = memory.NewNoOpEpisodicMemory()
	}

	wm := cfg.WorkingMemory
	if wm == nil {
		wm = memory.NewWorkingMemory()
	}

	return func(ctx context.Context, state graph.State) (any, error) {

		// Read goal from state, falling back to config
		goal := cfg.Goal
		if stateGoal, ok := graph.GetStateValue[string](state, StateKeyGoal); ok && stateGoal != "" {
			goal = stateGoal
		}
		logr := logger.GetLogger(ctx).With("fn", "agentNodeFunc", "goal", goal)

		// Read previous stage output so this stage knows what was already done.
		prevOutput, _ := graph.GetStateValue[string](state, StateKeyPreviousStageOutput)

		// Read adaptive-loop iteration context (accumulated output from prior iterations).
		iterationCtx, _ := graph.GetStateValue[string](state, StateKeyIterationContext)
		iterationCount, _ := graph.GetStateValue[int](state, StateKeyIterationCount)

		// Build prompt enriched with memory and previous stage context
		prompt := buildAgentPrompt(ctx, goal, wm, ep, cfg.WisdomStore, prevOutput, iterationCtx, iterationCount, cfg.BudgetExhaustedTools)

		logr.Info("agent node calling expert",
			"prompt_length", len(prompt),
			"iteration", iterationCount,
			"has_prev_output", prevOutput != "",
			"has_iteration_ctx", iterationCtx != "",
		)

		// Inject the goal into the context so downstream tools (e.g.
		// send_message) can record it in the reaction ledger for later
		// correlation with user emoji reactions.
		ctx = messenger.WithGoal(ctx, goal)

		var resp expert.Response
		var err error

		if cfg.SystemInstruction != "" && cfg.ModelProvider != nil {
			// Lightweight mode: build a minimal llmagent directly.
			// This avoids injecting the full codeowner persona which
			// causes sub-agents to hallucinate orchestration tools.
			resp, err = runLightweightAgent(ctx, prompt, cfg)
		} else {
			// Full expert mode: uses the codeowner persona.
			resp, err = cfg.Expert.Do(ctx, expert.Request{
				Message:         prompt,
				AdditionalTools: cfg.Tools,
				WorkingMemory:   wm,
				TaskType:        cfg.TaskType,
				Attachments:     cfg.Attachments,
			})
		}
		if err != nil {
			logr.Error("agent node call failed", "error", err)
			return graph.State{
				StateKeyNodeStatus: Failure,
				StateKeyOutput:     fmt.Sprintf("error: %v", err),
			}, nil // Return state update, not error, so graph can continue
		}

		// Count tool calls across ALL choices to detect terminal/budget state.
		// But only use the LAST choice's text content as the output.
		// The runner appends choices from every LLM generation in the session;
		// earlier choices contain tool-call-only turns whose "content" includes
		// echoed tool results (e.g. send_message's {"message_id":"...","status":"sent"})
		// that would pollute the output if concatenated.
		toolCallCount := 0
		toolCallCounts := make(map[string]int) // per-tool call counts for budget tracking
		allTerminal := true                    // true if every tool call is a "delivery" tool
		for _, choice := range resp.Choices {
			toolCallCount += len(choice.Message.ToolCalls)
			for _, tc := range choice.Message.ToolCalls {
				if !isTerminalTool(tc.Function.Name) {
					allTerminal = false
				}
				toolCallCounts[tc.Function.Name]++
			}
		}
		// Use only the last choice's content — the final LLM response after
		// all tool calls have been processed within the runner session.
		output := ""
		if len(resp.Choices) > 0 {
			output = resp.Choices[len(resp.Choices)-1].Message.Content
		}

		// Mark task as completed when:
		//   a) No tool calls were made (agent concluded with just text), OR
		//   b) ALL tool calls were "terminal" delivery tools (send_message,
		//      ask_clarifying_question). These tools represent the agent's
		//      final action — delivering a response to the user. Iterating
		//      again only causes redundant "working on it" messages and
		//      unnecessary follow-up questions.
		taskCompleted := toolCallCount == 0 || (toolCallCount > 0 && allTerminal)

		// Clear output when send_message was used — it already delivered the
		// response to the user, so the runner text is just mangled JSON.
		// For ask_clarifying_question, keep the output: the runner's final
		// generation after the Q&A is the actual useful response.
		if toolCallCounts[messenger.ToolName] > 0 && toolCallCounts[clarify.ToolName] == 0 {
			output = ""
		}

		// Store episodes in episodic memory for future reference.
		// Successful outputs are stored as pending (awaiting user validation).
		// Error-like outputs are stored as failures with verbal reflections.
		if output != "" && !looksLikeError(output) {
			// Cap trajectory to prevent large tool outputs from bloating
			// future prompts when this episode is retrieved.
			trajectory := output
			const maxTrajectoryRunes = 500
			runes := []rune(trajectory)
			if len(runes) > maxTrajectoryRunes {
				trajectory = string(runes[:maxTrajectoryRunes]) + "... (truncated)"
			}

			episode := memory.Episode{
				Goal:       goal,
				Trajectory: trajectory,
				Status:     memory.EpisodePending,
				Importance: scoreEpisodeImportance(ctx, cfg.ImportanceScorer, goal, trajectory, memory.EpisodePending),
			}
			ep.Store(ctx, episode)

			// Auto-store output into working memory so sibling/downstream
			// agents see it via prompt injection (trpc-agent-go pattern).
			// This replaces the scratchpad_write tool — no LLM call needed.
			wm.Store(goal, trajectory)
		} else if output != "" {
			// Failed output: generate a verbal reflection and store for learning.
			// This is the key change — instead of discarding failures, we learn
			// from them via the Reflexion pattern (verbal reinforcement).
			reflection := generateFailureReflection(ctx, goal, output, cfg.FailureReflector)
			episode := memory.Episode{
				Goal:       goal,
				Trajectory: output,
				Status:     memory.EpisodeFailure,
				Reflection: reflection,
				Importance: scoreEpisodeImportance(ctx, cfg.ImportanceScorer, goal, reflection, memory.EpisodeFailure),
			}
			ep.Store(ctx, episode)
			logr.Info("stored failure episode with reflection",
				"goal", goal,
				"has_reflection", reflection != "",
				"importance", episode.Importance,
				"output_prefix", toolwrap.TruncateForAudit(output, 100),
			)
		}

		logr.Info("agent node completed",
			"output_length", len(output),
			"tool_call_count", toolCallCount,
			"tool_call_counts", toolCallCounts,
			"all_terminal", allTerminal,
			"task_completed", taskCompleted,
		)

		// Forward usage from the response when available.
		newState := graph.State{
			StateKeyNodeStatus:     Success,
			StateKeyOutput:         output,
			StateKeyTaskCompleted:  taskCompleted,
			StateKeyToolCallCounts: toolCallCounts,
		}

		if resp.Usage != nil {
			usage := resp.Usage
			budgetEvt := hooks.ContextBudgetEvent{
				PersonaTokens:    0,
				ToolSchemaTokens: 0,
				HistoryTokens:    usage.PromptTokens,
				TotalTokens:      usage.TotalTokens,
				// maxTokens is harder to get here but we can default
				MaxTokens:      128000,
				UtilizationPct: float64(usage.TotalTokens) / 128000.0,
			}
			newState[StateKeyContextBudget] = budgetEvt

			// Emit ContextBudget telemetry for the full-expert path as well.
			if cfg.Hooks != nil {
				cfg.Hooks.OnContextBudget(ctx, budgetEvt)
			}
		} else if existingBudget, ok := state[StateKeyContextBudget]; ok {
			// Preserve any previously recorded context budget when no new usage is available.
			newState[StateKeyContextBudget] = existingBudget
		}

		return newState, nil
	}
}

// buildAgentPrompt constructs the prompt for the expert, incorporating
// working memory context, episodic memories, previous stage output, and
// adaptive-loop iteration context.
func buildAgentPrompt(
	ctx context.Context,
	goal string,
	wm *memory.WorkingMemory,
	ep memory.EpisodicMemory,
	ws memory.WisdomStore,
	previousStageOutput string,
	iterationContext string,
	iterationCount int,
	budgetExhaustedTools []string,
) string {
	var sb strings.Builder

	// Include adaptive-loop iteration context (accumulated output from prior iterations).
	// This takes priority over previousStageOutput when both are present.
	if iterationContext != "" {
		fmt.Fprintf(&sb, "## Progress So Far (iteration %d)\n", iterationCount)
		sb.WriteString("The following was already accomplished in prior iterations. " +
			"The results below were ALREADY SHOWN to the user via streaming. " +
			"DO NOT repeat, rephrase, or re-output this data. " +
			"DO NOT repeat tool calls or research already done. " +
			"If the task is complete based on the information below, say ONLY " +
			"a brief confirmation like 'Done' or 'Here is the summary' without repeating the data. " +
			"Only produce NEW information or actions not covered below.\n\n")
		const maxIterCtx = 4000
		if len(iterationContext) > maxIterCtx {
			// Keep the tail (most recent work) rather than the head
			sb.WriteString("... (earlier output truncated)\n")
			sb.WriteString(iterationContext[len(iterationContext)-maxIterCtx:])
		} else {
			sb.WriteString(iterationContext)
		}
		sb.WriteString("\n\n")
	} else if previousStageOutput != "" {
		// Fallback for multi-stage mode (backward compatibility)
		sb.WriteString("## Previous Stage Output\n")
		sb.WriteString("The following was already accomplished in the previous stage. " +
			"DO NOT repeat this work. Build upon it instead.\n\n")
		sb.WriteString(previousStageOutput)
		sb.WriteString("\n\n")
	}

	// Inject a hard stop when tool budgets are exhausted.
	// This supplements the tool removal: even if the LLM wanted to call them,
	// the tools won't be available, but this hint steers it proactively.
	if len(budgetExhaustedTools) > 0 {
		sb.WriteString("## IMPORTANT: Tool Budget Exhausted\n")
		fmt.Fprintf(&sb, "The following tools have been removed because their call budget is exhausted: %s. ",
			strings.Join(budgetExhaustedTools, ", "))
		sb.WriteString("Do NOT attempt to use them. " +
			"Use sensible defaults for any remaining unknowns and proceed to complete the task immediately.\n\n")
	}

	snapshot := wm.Snapshot()
	logger.GetLogger(ctx).Debug("buildAgentPrompt: memory context",
		"working_memory_keys", len(snapshot),
		"has_iteration_ctx", iterationContext != "",
		"has_prev_output", previousStageOutput != "",
	)
	if len(snapshot) > 0 {
		var subagentResults []string
		var otherMemories []string

		for k, v := range snapshot {
			vs := fmt.Sprintf("%v", v)
			if strings.HasPrefix(k, "subagent:") && strings.HasSuffix(k, ":result") {
				name := strings.TrimSuffix(strings.TrimPrefix(k, "subagent:"), ":result")
				subagentResults = append(subagentResults, fmt.Sprintf("### Data from Sub-Agent %q:\n%s\n", name, vs))
			} else if strings.HasPrefix(k, "plan_step:") && strings.HasSuffix(k, ":result") {
				name := strings.TrimSuffix(strings.TrimPrefix(k, "plan_step:"), ":result")
				subagentResults = append(subagentResults, fmt.Sprintf("### Data from Plan Step %q:\n%s\n", name, vs))
			} else {
				otherMemories = append(otherMemories, fmt.Sprintf("- %s: %s\n", k, vs))
			}
		}

		if len(subagentResults) > 0 {
			sb.WriteString("## Sub-Agent Results\n")
			sb.WriteString("The following sub-agents have ALREADY RUN and gathered this information. ")
			sb.WriteString("DO NOT SPAWN them again for the same goal. Use this data to answer the user's request immediately.\n\n")
			for _, res := range subagentResults {
				// Prevent extreme bloat per sub-agent result
				resRunes := []rune(res)
				if len(resRunes) > 2000 {
					truncated := string(resRunes[:2000])
					sb.WriteString(truncated + "\n... (remaining data omitted for brevity)\n\n")
				} else {
					sb.WriteString(res + "\n")
				}
			}
		}

		if len(otherMemories) > 0 {
			sb.WriteString("## Working Memory (shared observations)\n")
			const maxSnapshotSize = 2000
			const maxEntrySize = 300
			snapshotLen := 0

			for _, entry := range otherMemories {
				entryRunes := []rune(entry)
				if len(entryRunes) > maxEntrySize {
					entry = string(entryRunes[:maxEntrySize-len("...\n")]) + "...\n"
				}
				if snapshotLen+len(entry) > maxSnapshotSize {
					sb.WriteString("- ... (additional entries omitted for brevity)\n")
					break
				}
				sb.WriteString(entry)
				snapshotLen += len(entry)
			}
			sb.WriteString("\n")
		}
	}

	// Include episodic memory (past similar experiences) with weighted retrieval.
	// RetrieveWeighted ranks by recency (exponential decay) × importance,
	// so recent lessons surface first and old ones naturally fade away.
	episodes := ep.RetrieveWeighted(ctx, goal, 3)
	if len(episodes) > 0 {
		sb.WriteString("## Relevant Past Experiences\n")
		for _, ep := range episodes {
			sb.WriteString(ep.String())
			sb.WriteString("\n\n")
		}
	}

	// Include consolidated daily wisdom notes.
	// These are distilled summaries from the daily consolidation job,
	// providing high-level lessons without raw episode clutter.
	if ws != nil {
		notes := ws.RetrieveWisdom(ctx, 3)
		wisdomSection := memory.FormatWisdomForPrompt(notes)
		if wisdomSection != "" {
			sb.WriteString(wisdomSection)
		}
	}

	// The main task
	sb.WriteString("## Current Task\n")
	sb.WriteString(goal)

	return sb.String()
}

// runLightweightAgent creates a minimal sub-agent with only the given
// SystemInstruction — no full codeowner persona. This prevents plan-step
// sub-agents from hallucinating orchestration tools (create_agent, etc.)
// that appear in the persona but are not in their tool set.
func runLightweightAgent(ctx context.Context, prompt string, cfg AgentNodeConfig) (expert.Response, error) {
	logr := logger.GetLogger(ctx).With("fn", "runLightweightAgent", "goal", cfg.Goal)

	taskType := cfg.TaskType
	if taskType == "" {
		taskType = modelprovider.TaskPlanning
	}

	modelToUse, err := cfg.ModelProvider.GetModel(ctx, taskType)
	if err != nil {
		return expert.Response{}, fmt.Errorf("failed to get model for lightweight agent: %w", err)
	}

	maxLLMCalls := cfg.MaxDecisions
	if maxLLMCalls < 8 {
		maxLLMCalls = 8
	}
	const maxLLMCallsCap = 15
	if maxLLMCalls > maxLLMCallsCap {
		maxLLMCalls = maxLLMCallsCap
	}

	subAgent := llmagent.New(
		"plan-step",
		llmagent.WithModels(modelToUse),
		llmagent.WithTools(cfg.Tools),
		llmagent.WithInstruction(cfg.SystemInstruction),
		llmagent.WithDescription("Focused plan-step sub-agent"),
		llmagent.WithEnableParallelTools(true),
		llmagent.WithMaxLLMCalls(maxLLMCalls),
		llmagent.WithAddCurrentTime(true),
		llmagent.WithTimeFormat(time.RFC3339),
		llmagent.WithMaxToolIterations(maxLLMCalls), // same budget
		llmagent.WithMessageFilterMode(llmagent.RequestContext),
	)
	summarizationModel, _ := cfg.ModelProvider.GetModel(ctx, modelprovider.TaskEfficiency)

	sessionSvc := inmemory.NewSessionService(
		inmemory.WithSummarizer(summary.NewSummarizer(
			summarizationModel.GetAny(),
			summary.WithTokenThreshold(2000),
			summary.WithName("plan-step-summarizer"),
		)),
	)
	r := runner.NewRunner("plan-step", subAgent,
		runner.WithSessionService(sessionSvc),
	)
	defer func() { _ = r.Close() }()

	const stepTimeout = 2 * time.Minute
	runCtx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()

	var evCh <-chan *event.Event
	runErr := retrier.Retry(runCtx, func() error {
		var runErr error
		evCh, runErr = r.Run(runCtx, "plan", "plan-step", model.NewUserMessage(prompt))
		return runErr
	},
		retrier.WithAttempts(3),
		retrier.WithBackoffDuration(5*time.Second),
		retrier.WithRetryIf(expert.IsTransientError),
	)
	if runErr != nil {
		return expert.Response{}, fmt.Errorf("lightweight agent run failed: %w", runErr)
	}

	// Collect response content from events.
	var choices []model.Choice
	var lastUsage *model.Usage
	for ev := range evCh {
		if ev.Error != nil {
			return expert.Response{}, fmt.Errorf("lightweight agent error: %s", ev.Error.Message)
		}
		if ev.Response != nil {
			choices = append(choices, ev.Choices...)
			if ev.Usage != nil {
				lastUsage = ev.Usage
			}
		}
	}

	if lastUsage != nil {
		maxTokens := 128_000 // Fallback assuming a typical context window sizes
		pct := float64(lastUsage.TotalTokens) / float64(maxTokens)

		if cfg.Hooks != nil {
			cfg.Hooks.OnContextBudget(ctx, hooks.ContextBudgetEvent{
				PersonaTokens:    0,
				ToolSchemaTokens: 0,
				HistoryTokens:    lastUsage.PromptTokens, // we don't have the exact breakdown from trpc so map it all to history
				TotalTokens:      lastUsage.TotalTokens,
				MaxTokens:        maxTokens,
				UtilizationPct:   pct,
			})
		}

		if pct > 0.85 {
			logr.Warn(fmt.Sprintf("context budget at %d%%, consider reducing persona or compacting history", int(pct*100)),
				"prompt_tokens", lastUsage.PromptTokens,
				"total_tokens", lastUsage.TotalTokens,
			)
		} else {
			logr.Info("context budget update",
				"prompt_tokens", lastUsage.PromptTokens,
				"total_tokens", lastUsage.TotalTokens,
				"pct", int(pct*100),
			)
		}
	}

	logr.Info("lightweight agent completed", "choices", len(choices))

	return expert.Response{
		Choices: choices,
		Usage:   lastUsage,
	}, nil
}

// terminalTools are tools that represent the agent's final action — delivering
// a response to the user via messaging. When an iteration's ONLY tool calls
// are terminal tools, the adaptive loop should stop because there is no more
// computational work to iterate on; the agent has already communicated its
// answer (or asked for more info).
var terminalTools = map[string]bool{
	messenger.ToolName: true,
	clarify.ToolName:   true,
	// ask_clarifying_question IS terminal because the Q&A round-trip
	// happens within the same runner session — the LLM asks, gets the
	// answer via tool result, then produces its final response. Marking
	// it terminal stops the adaptive loop from creating a new session
	// (which loses the Q&A context and causes repeated questions).
}

// isTerminalTool returns true if the named tool is a "delivery" tool that
// represents a final user-facing action.
func isTerminalTool(name string) bool {
	return terminalTools[name]
}
