package reactree

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/go-lib/logger"
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

		// Build prompt enriched with memory
		prompt := buildAgentPrompt(ctx, goal, wm, ep)

		logr.Debug("agent node calling expert", "prompt_length", len(prompt))

		resp, err := cfg.Expert.Do(ctx, expert.Request{
			Message:         prompt,
			EventChannel:    cfg.EventChan,
			AdditionalTools: cfg.Tools,
			WorkingMemory:   wm,
		})
		if err != nil {
			logr.Error("agent node expert call failed", "error", err)
			return graph.State{
				StateKeyNodeStatus: Failure,
				StateKeyOutput:     fmt.Sprintf("error: %v", err),
			}, nil // Return state update, not error, so graph can continue
		}

		// Collect response text
		var responseText strings.Builder
		for _, choice := range resp.Choices {
			responseText.WriteString(choice.Message.Content)
		}
		output := responseText.String()

		// Store in episodic memory
		ep.Store(ctx, memory.Episode{
			Goal:       goal,
			Trajectory: output,
			Status:     memory.EpisodeSuccess,
		})

		logr.Debug("agent node completed", "output_length", len(output))

		return graph.State{
			StateKeyNodeStatus: Success,
			StateKeyOutput:     output,
		}, nil
	}
}

// buildAgentPrompt constructs the prompt for the expert, incorporating
// working memory context and any relevant episodic memories.
func buildAgentPrompt(ctx context.Context, goal string, wm *memory.WorkingMemory, ep memory.EpisodicMemory) string {
	var sb strings.Builder

	// Include working memory context if available (capped to prevent prompt bloat)
	snapshot := wm.Snapshot()
	if len(snapshot) > 0 {
		sb.WriteString("## Working Memory (shared observations)\n")
		const maxSnapshotSize = 4000
		const maxEntrySize = 500
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
	episodes := ep.Retrieve(ctx, goal, 3)
	if len(episodes) > 0 {
		sb.WriteString("## Relevant Past Experiences\n")
		for _, ep := range episodes {
			sb.WriteString(fmt.Sprintf("### Goal: %s (outcome: %s)\n%s\n\n", ep.Goal, ep.Status, ep.Trajectory))
		}
	}

	// The main task
	sb.WriteString("## Current Task\n")
	sb.WriteString(goal)

	return sb.String()
}
