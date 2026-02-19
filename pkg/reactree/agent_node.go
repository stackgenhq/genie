package reactree

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// AgentNodeConfig holds configuration for creating an agent node function.
type AgentNodeConfig struct {
	Goal          string
	Expert        expert.Expert
	WorkingMemory *memory.WorkingMemory
	Episodic      memory.EpisodicMemory
	MaxDecisions  int
	EventChan     chan<- interface{}
	Tools         []tool.Tool
	// TaskType selects the model for this node via ModelProvider.GetModel().
	// If empty, defaults to TaskPlanning.
	TaskType modelprovider.TaskType

	// SenderContext identifies the user/channel for this request.
	SenderContext string
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
		logr := logger.GetLogger(ctx).With("fn", "agentNodeFunc", "goal", cfg.Goal)

		// Read goal from state, falling back to config
		goal := cfg.Goal
		if stateGoal, ok := graph.GetStateValue[string](state, StateKeyGoal); ok && stateGoal != "" {
			goal = stateGoal
		}

		// Read previous stage output so this stage knows what was already done.
		prevOutput, _ := graph.GetStateValue[string](state, StateKeyPreviousStageOutput)

		// Read adaptive-loop iteration context (accumulated output from prior iterations).
		iterationCtx, _ := graph.GetStateValue[string](state, StateKeyIterationContext)
		iterationCount, _ := graph.GetStateValue[int](state, StateKeyIterationCount)

		// Build prompt enriched with memory and previous stage context
		prompt := buildAgentPrompt(ctx, goal, wm, ep, prevOutput, iterationCtx, iterationCount)

		logr.Info("agent node calling expert",
			"prompt_length", len(prompt),
			"iteration", iterationCount,
			"has_prev_output", prevOutput != "",
			"has_iteration_ctx", iterationCtx != "",
		)

		resp, err := cfg.Expert.Do(ctx, expert.Request{
			Message:         prompt,
			EventChannel:    cfg.EventChan,
			AdditionalTools: cfg.Tools,
			WorkingMemory:   wm,
			TaskType:        cfg.TaskType,
			SenderContext:   cfg.SenderContext,
		})
		if err != nil {
			logr.Error("agent node expert call failed", "error", err)
			return graph.State{
				StateKeyNodeStatus: Failure,
				StateKeyOutput:     fmt.Sprintf("error: %v", err),
			}, nil // Return state update, not error, so graph can continue
		}

		// Collect response text and count tool calls to detect early completion.
		var responseText strings.Builder
		toolCallCount := 0
		for _, choice := range resp.Choices {
			responseText.WriteString(choice.Message.Content)
			toolCallCount += len(choice.Message.ToolCalls)
		}
		output := responseText.String()

		// Only store successful episodes in episodic memory.
		// Skip error-like responses that would poison future context.
		if !looksLikeError(output) {
			// Cap trajectory to prevent large tool outputs from bloating
			// future prompts when this episode is retrieved.
			trajectory := output
			const maxTrajectorySize = 500
			if len(trajectory) > maxTrajectorySize {
				trajectory = trajectory[:maxTrajectorySize] + "... (truncated)"
			}
			ep.Store(ctx, memory.Episode{
				Goal:       goal,
				Trajectory: trajectory,
				Status:     memory.EpisodeSuccess,
			})
		} else {
			logr.Warn("skipping episodic memory storage for error-like output", "output_prefix", toolwrap.TruncateForAudit(output, 100))
		}

		// If zero tool calls were made, the agent concluded with just text.
		// Mark the task as completed so the stage router can skip remaining stages.
		taskCompleted := toolCallCount == 0
		logr.Info("agent node completed",
			"output_length", len(output),
			"tool_call_count", toolCallCount,
			"task_completed", taskCompleted,
		)

		return graph.State{
			StateKeyNodeStatus:    Success,
			StateKeyOutput:        output,
			StateKeyTaskCompleted: taskCompleted,
		}, nil
	}
}

// buildAgentPrompt constructs the prompt for the expert, incorporating
// working memory context, episodic memories, previous stage output, and
// adaptive-loop iteration context.
func buildAgentPrompt(ctx context.Context, goal string, wm *memory.WorkingMemory, ep memory.EpisodicMemory, previousStageOutput string, iterationContext string, iterationCount int) string {
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
		const maxPrevOutput = 2000
		if len(previousStageOutput) > maxPrevOutput {
			sb.WriteString(previousStageOutput[:maxPrevOutput])
			sb.WriteString("\n... (truncated)\n")
		} else {
			sb.WriteString(previousStageOutput)
		}
		sb.WriteString("\n\n")
	}

	// Include working memory context if available (capped to prevent prompt bloat)
	snapshot := wm.Snapshot()
	logger.GetLogger(ctx).Debug("buildAgentPrompt: memory context",
		"working_memory_keys", len(snapshot),
		"has_iteration_ctx", iterationContext != "",
		"has_prev_output", previousStageOutput != "",
	)
	if len(snapshot) > 0 {
		sb.WriteString("## Working Memory (shared observations)\n")
		const maxSnapshotSize = 2000
		const maxEntrySize = 300
		snapshotLen := 0
		for k, v := range snapshot {
			vs := fmt.Sprintf("%v", v)
			entry := fmt.Sprintf("- %s: %s\n", k, vs)
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

	// Include episodic memory (past similar experiences)
	episodes := ep.Retrieve(ctx, goal, 2)
	if len(episodes) > 0 {
		sb.WriteString("## Relevant Past Experiences\n")
		for _, ep := range episodes {
			fmt.Fprintf(&sb, "### Goal: %s (outcome: %s)\n%s\n\n", ep.Goal, ep.Status, ep.Trajectory)
		}
	}

	// The main task
	sb.WriteString("## Current Task\n")
	sb.WriteString(goal)

	return sb.String()
}
