// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package reactree

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/clarify"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// StageConfig defines a named stage in a multi-stage ReAcTree.
// Each stage becomes an agent node in a sequence graph. The stage instruction
// is appended to the goal to guide the LLM's focus for that stage.
type StageConfig struct {
	// Name is the display name shown in the TUI progress bar (e.g., "Understanding").
	Name string
	// Instruction is appended to the goal for this stage (optional).
	// Example: "Read relevant files and gather context before acting."
	Instruction string
	// TaskType selects the LLM model for this stage via ModelProvider.GetModel().
	// If empty, the expert's default (TaskPlanning) is used.
	// Example: modelprovider.TaskToolCalling for execution-heavy stages.
	TaskType modelprovider.TaskType
}

// TreeConfig holds configuration for a ReAcTree execution run.
// These limits prevent runaway tree growth and unbounded LLM calls.
type TreeConfig struct {
	// MaxDepth limits how deep the tree can grow through recursive expansion.
	MaxDepth int

	// MaxDecisionsPerNode limits how many LLM calls a single agent node can make.
	MaxDecisionsPerNode int

	// MaxTotalNodes limits the total number of nodes in the tree.
	MaxTotalNodes int

	// Stages defines the named stages for multi-stage execution.
	// If empty, a single root node is built (backward compatible).
	// If set, a sequence graph is built with one agent node per stage.
	// Deprecated: prefer MaxIterations for adaptive loop execution.
	Stages []StageConfig

	// MaxIterations sets the maximum number of adaptive-loop iterations.
	// When > 0 (and Stages is empty), the executor runs a single agent node
	// in a loop that accumulates context and terminates when the LLM produces
	// zero tool calls or the iteration cap is reached. This replaces the fixed
	// stage pipeline with dynamic, task-driven expansion.
	MaxIterations int

	// ToolBudgets limits how many times specific tools can be called across
	// all iterations of the adaptive loop. When a tool's budget is exhausted,
	// it is removed from the tool list so the LLM is forced to proceed
	// without it. Example: {"ask_clarifying_question": 1} ensures the agent
	// asks at most one clarifying question before using defaults.
	ToolBudgets map[string]int

	// Toggles holds opt-in configurations for predictability, bounding,
	// and enterprise readiness.
	Toggles Toggles

	// Checkpointer is used to save and restore execution state.
	Checkpointer graph.CheckpointSaver `json:"-"`
}

// DefaultTreeConfig returns sensible defaults for tree execution.
func DefaultTreeConfig() TreeConfig {
	return TreeConfig{
		MaxDepth:            3,
		MaxDecisionsPerNode: 10,
		MaxTotalNodes:       30,
		MaxIterations:       3,
		ToolBudgets: map[string]int{
			clarify.ToolName: 1,
			// Limit sub-agent spawning to prevent the LLM from creating
			// endless swarms when tools (like web_search) are failing.
			// 3 calls ≈ one multi-step plan + two focused retries.
			"create_agent": 3,
		},
	}
}

// TreeResult captures the outcome of a complete ReAcTree execution run.
type TreeResult struct {
	Status        NodeStatus
	Output        string
	NodeCount     int
	ContextBudget hooks.ContextBudgetEvent
}

//go:generate go tool counterfeiter -generate

// TreeExecutor orchestrates a full ReAcTree run from a top-level goal.
// It builds a graph.StateGraph, compiles it, creates a graph.Executor,
// and runs it — delegating all orchestration to trpc-agent-go's graph package.
//
//counterfeiter:generate . TreeExecutor
type TreeExecutor interface {
	// Run executes a ReAcTree for the given goal and returns the result.
	Run(ctx context.Context, req TreeRequest) (TreeResult, error)
}

// TreeRequest contains all inputs for a single tree execution.
type TreeRequest struct {
	Goal       string
	Tools      []tool.Tool
	ToolGetter func() []tool.Tool
	TaskType   modelprovider.TaskType
	// Attachments are file/media attachments from the incoming message.
	// Image attachments are passed as multimodal content to the LLM.
	Attachments []messenger.Attachment

	// WorkingMemory overrides the tree-level working memory for this request.
	// When set, enables per-sender memory isolation. If nil, falls back to
	// the tree's shared working memory.
	WorkingMemory *memory.WorkingMemory

	// EpisodicMemory overrides the tree-level episodic memory for this request.
	// When set, enables per-sender episode isolation. If nil, falls back to
	// the tree's shared episodic memory.
	EpisodicMemory memory.EpisodicMemory
}

// tree is the default TreeExecutor implementation backed by graph.StateGraph.
type tree struct {
	expert        expert.Expert
	workingMemory *memory.WorkingMemory
	episodic      memory.EpisodicMemory
	config        TreeConfig
	reflector     ActionReflector
	hooks         hooks.ExecutionHook
}

// NewTreeExecutor creates a TreeExecutor configured with the given expert and options.
// The expert is used as the LLM backend for all agent nodes in the tree.
func NewTreeExecutor(
	exp expert.Expert,
	workingMem *memory.WorkingMemory,
	episodic memory.EpisodicMemory,
	config TreeConfig,
) TreeExecutor {
	if workingMem == nil {
		workingMem = memory.NewWorkingMemory()
	}
	if episodic == nil {
		episodic = memory.NewNoOpEpisodicMemory()
	}
	// Resolve enterprise dependencies.
	var reflector ActionReflector = &NoOpReflector{}
	if config.Toggles.EnableActionReflection && config.Toggles.Reflector != nil {
		reflector = config.Toggles.Reflector
	}

	execHooks := config.Toggles.Hooks
	if execHooks == nil {
		execHooks = hooks.NoOpHook{}
	}

	logger.GetLogger(context.Background()).Info("TreeExecutor created",
		"max_depth", config.MaxDepth,
		"max_decisions_per_node", config.MaxDecisionsPerNode,
		"max_total_nodes", config.MaxTotalNodes,
		"max_iterations", config.MaxIterations,
		"stages", len(config.Stages),
		"enterprise.critic", config.Toggles.EnableCriticMiddleware,
		"enterprise.reflection", config.Toggles.EnableActionReflection,
		"enterprise.dry_run", config.Toggles.EnableDryRunSimulation,
		"enterprise.mcp", config.Toggles.EnableMCPServerAccess,
		"enterprise.audit", config.Toggles.EnableAuditDashboard,
		"enterprise.hooks", execHooks != nil,
	)
	return &tree{
		expert:        exp,
		workingMemory: workingMem,
		episodic:      episodic,
		config:        config,
		reflector:     reflector,
		hooks:         execHooks,
	}
}

// resolveWorkingMemory returns the per-request working memory if set,
// otherwise falls back to the tree-level shared instance.
func (t *tree) resolveWorkingMemory(req TreeRequest) *memory.WorkingMemory {
	if req.WorkingMemory != nil {
		return req.WorkingMemory
	}
	return t.workingMemory
}

// resolveEpisodic returns the per-request episodic memory if set,
// otherwise falls back to the tree-level shared instance.
func (t *tree) resolveEpisodic(req TreeRequest) memory.EpisodicMemory {
	if req.EpisodicMemory != nil {
		return req.EpisodicMemory
	}
	return t.episodic
}

// Run builds a StateGraph for the goal, compiles, and executes it.
// Routing priority:
//  1. MaxIterations > 0 (and no Stages) → adaptive loop
//  2. Stages configured → multi-stage sequence (legacy)
//  3. Neither → single root node (backward compatible)
func (t *tree) Run(ctx context.Context, req TreeRequest) (TreeResult, error) {
	if t.config.MaxIterations > 0 && len(t.config.Stages) == 0 {
		return t.runAdaptiveLoop(ctx, req)
	}
	if len(t.config.Stages) > 0 {
		return t.runMultiStage(ctx, req)
	}
	return t.runSingleNode(ctx, req)
}

// runSingleNode is the original single-node execution path (backward compatible).
func (t *tree) runSingleNode(ctx context.Context, req TreeRequest) (TreeResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "tree.Run", "goal", req.Goal)
	logr.Info("starting ReAcTree execution")

	// Build a single-node graph for the root goal
	schema := NewReAcTreeSchema()
	sg := graph.NewStateGraph(schema)

	// Capture output via closure — the graph executor doesn't expose the
	// final state, so we intercept the agent node's result directly.
	var capturedOutput string
	var capturedStatus NodeStatus

	toolsToUse := req.Tools

	// Enterprise: wrap tools with critic middleware if enabled.
	if t.config.Toggles.EnableCriticMiddleware {
		validator := NewDeterministicValidator(nil)
		wrapped := make([]tool.Tool, len(toolsToUse))
		for i, tl := range toolsToUse {
			wrapped[i] = WrapWithValidator(tl, validator)
		}
		toolsToUse = wrapped
	}

	// Enterprise: wrap tools for dry run simulation if enabled.
	if t.config.Toggles.EnableDryRunSimulation {
		wrapped, _ := WrapToolsForDryRun(toolsToUse)
		toolsToUse = wrapped
	}

	innerFunc := NewAgentNodeFunc(AgentNodeConfig{
		Goal:          req.Goal,
		Expert:        t.expert,
		WorkingMemory: t.resolveWorkingMemory(req),
		Episodic:      t.resolveEpisodic(req),
		MaxDecisions:  t.config.MaxDecisionsPerNode,
		Tools:         toolsToUse,
		Attachments:   req.Attachments,
		Hooks:         t.hooks,
	})

	wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
		result, err := innerFunc(ctx, state)
		if err != nil {
			return result, err
		}
		// Capture the output from the returned state update
		if stateMap, ok := result.(graph.State); ok {
			if out, ok := stateMap[StateKeyOutput].(string); ok {
				capturedOutput = out
			}
			if status, ok := stateMap[StateKeyNodeStatus].(NodeStatus); ok {
				capturedStatus = status
			}
		}
		return result, nil
	}

	sg.AddNode("root", wrappedFunc).
		SetEntryPoint("root").
		SetFinishPoint("root")

	compiled, err := sg.Compile()
	if err != nil {
		return TreeResult{Status: Failure}, fmt.Errorf("failed to compile ReAcTree graph: %w", err)
	}

	executor, err := graph.NewExecutor(compiled,
		graph.WithMaxSteps(t.config.MaxTotalNodes),
	)
	if err != nil {
		return TreeResult{Status: Failure}, fmt.Errorf("failed to create ReAcTree executor: %w", err)
	}

	// Build initial state
	initialState := graph.State{
		StateKeyGoal: req.Goal,
	}

	// Execute the graph — drain events to ensure completion.
	events, err := executor.Execute(ctx, initialState, agent.NewInvocation())
	if err != nil {
		return TreeResult{Status: Failure}, fmt.Errorf("ReAcTree execution failed: %w", err)
	}
	for range events {
		// Drain graph lifecycle events to let execution finish
	}

	result := TreeResult{
		Status:    capturedStatus,
		Output:    capturedOutput,
		NodeCount: 1,
	}

	logr.Info("ReAcTree execution completed", "status", result.Status, "output_length", len(result.Output))
	return result, nil
}

// runMultiStage builds a multi-node sequence graph from the configured stages.
// Each stage is an agent node that receives the goal (enriched with the stage
// instruction) and shares working memory with other stages. Stage transitions
// emit TUI progress events.
func (t *tree) runMultiStage(ctx context.Context, req TreeRequest) (TreeResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "tree.RunMultiStage", "goal", req.Goal, "stages", len(t.config.Stages))
	logr.Info("starting multi-stage ReAcTree execution")

	schema := NewReAcTreeSchema()
	sg := graph.NewStateGraph(schema)

	totalStages := len(t.config.Stages)
	nodeIDs := make([]string, totalStages)

	// Capture output from the last stage
	var capturedOutput string
	var capturedStatus NodeStatus

	for i, stage := range t.config.Stages {
		nodeID := fmt.Sprintf("stage_%d_%s", i, stage.Name)
		nodeIDs[i] = nodeID

		// Build the stage goal: original goal + stage-specific instruction
		stageGoal := req.Goal
		if stage.Instruction != "" {
			stageGoal = fmt.Sprintf(
				"## Stage: %s (%d/%d)\n%s\n\n## Goal\n%s",
				stage.Name, i+1, totalStages, stage.Instruction, req.Goal,
			)
		}

		stageIdx := i
		stageName := stage.Name

		toolsToUse := req.Tools
		if req.ToolGetter != nil {
			toolsToUse = req.ToolGetter()
		}

		// Enterprise: wrap tools with critic middleware if enabled.
		if t.config.Toggles.EnableCriticMiddleware {
			validator := NewDeterministicValidator(nil)
			wrapped := make([]tool.Tool, len(toolsToUse))
			for j, tl := range toolsToUse {
				wrapped[j] = WrapWithValidator(tl, validator)
			}
			toolsToUse = wrapped
		}

		// Enterprise: wrap tools for dry run simulation if enabled.
		if t.config.Toggles.EnableDryRunSimulation {
			wrapped, _ := WrapToolsForDryRun(toolsToUse)
			toolsToUse = wrapped
		}

		innerFunc := NewAgentNodeFunc(AgentNodeConfig{
			Goal:          stageGoal,
			Expert:        t.expert,
			WorkingMemory: t.resolveWorkingMemory(req),
			Episodic:      t.resolveEpisodic(req),
			MaxDecisions:  t.config.MaxDecisionsPerNode,
			Tools:         toolsToUse,
			TaskType:      stage.TaskType,
			Attachments:   req.Attachments,
			Hooks:         t.hooks,
		})

		// Wrap the node func to emit stage events and capture the last output
		wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
			// Emit stage progress to TUI (guard against no registered channel)
			if agui.ChannelFor(ctx) != nil {
				agui.EmitStageProgress(ctx, stageName, stageIdx, totalStages)
				agui.EmitThinking(ctx, "orchestrator", stageName+"...")
			}
			logr.Info("stage started", "stage", stageName, "index", stageIdx)

			result, err := innerFunc(ctx, state)
			if err != nil {
				logr.Error("stage failed", "stage", stageName, "error", err)
				return result, err
			}

			// Capture output from the returned state and propagate it
			// as PreviousStageOutput so the next stage avoids redundant work.
			if stateMap, ok := result.(graph.State); ok {
				if out, ok := stateMap[StateKeyOutput].(string); ok {
					capturedOutput = out
					// Pass this stage's output to the next stage via state.
					stateMap[StateKeyPreviousStageOutput] = out
				}
				if status, ok := stateMap[StateKeyNodeStatus].(NodeStatus); ok {
					capturedStatus = status
				}
			}

			logr.Info("stage completed", "stage", stageName, "index", stageIdx)
			return result, nil
		}

		sg.AddNode(nodeID, wrappedFunc)
	}

	// Wire the stages into a sequence with early-exit support:
	// if a stage completes without tool calls (task done), skip remaining stages.
	sg.SetEntryPoint(nodeIDs[0])
	BuildSequenceWithEarlyExit(sg, nodeIDs)

	compiled, err := sg.Compile()
	if err != nil {
		return TreeResult{Status: Failure}, fmt.Errorf("failed to compile multi-stage ReAcTree graph: %w", err)
	}

	executor, err := graph.NewExecutor(compiled,
		graph.WithMaxSteps(t.config.MaxTotalNodes),
	)
	if err != nil {
		return TreeResult{Status: Failure}, fmt.Errorf("failed to create multi-stage ReAcTree executor: %w", err)
	}

	initialState := graph.State{
		StateKeyGoal: req.Goal,
	}

	events, err := executor.Execute(ctx, initialState, agent.NewInvocation())
	if err != nil {
		return TreeResult{Status: Failure}, fmt.Errorf("multi-stage ReAcTree execution failed: %w", err)
	}
	for range events {
		// Drain graph lifecycle events to let execution finish
	}

	result := TreeResult{
		Status:    capturedStatus,
		Output:    capturedOutput,
		NodeCount: totalStages,
	}

	logr.Info("multi-stage ReAcTree execution completed",
		"status", result.Status,
		"stages", totalStages,
		"output_length", len(result.Output),
	)
	return result, nil
}

func (t *tree) runAdaptiveLoop(ctx context.Context, req TreeRequest) (TreeResult, error) {
	return t.runAdaptiveLoop_v2(ctx, req)
}
