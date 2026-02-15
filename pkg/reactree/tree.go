package reactree

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/go-lib/logger"
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
}

// DefaultTreeConfig returns sensible defaults for tree execution.
func DefaultTreeConfig() TreeConfig {
	return TreeConfig{
		MaxDepth:            3,
		MaxDecisionsPerNode: 10,
		MaxTotalNodes:       20,
		MaxIterations:       10,
	}
}

// TreeResult captures the outcome of a complete ReAcTree execution run.
type TreeResult struct {
	Status    NodeStatus
	Output    string
	NodeCount int
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
	Goal          string
	EventChan     chan<- interface{}
	Tools         []tool.Tool
	SenderContext string
}

// tree is the default TreeExecutor implementation backed by graph.StateGraph.
type tree struct {
	expert        expert.Expert
	workingMemory *memory.WorkingMemory
	episodic      memory.EpisodicMemory
	config        TreeConfig
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
	return &tree{
		expert:        exp,
		workingMemory: workingMem,
		episodic:      episodic,
		config:        config,
	}
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

	innerFunc := NewAgentNodeFunc(AgentNodeConfig{
		Goal:          req.Goal,
		Expert:        t.expert,
		WorkingMemory: t.workingMemory,
		Episodic:      t.episodic,
		MaxDecisions:  t.config.MaxDecisionsPerNode,
		EventChan:     req.EventChan,
		Tools:         req.Tools,
		SenderContext: req.SenderContext,
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

		innerFunc := NewAgentNodeFunc(AgentNodeConfig{
			Goal:          stageGoal,
			Expert:        t.expert,
			WorkingMemory: t.workingMemory,
			Episodic:      t.episodic,
			MaxDecisions:  t.config.MaxDecisionsPerNode,
			EventChan:     req.EventChan,
			Tools:         req.Tools,
			TaskType:      stage.TaskType,
			SenderContext: req.SenderContext,
		})

		// Wrap the node func to emit stage events and capture the last output
		wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
			// Emit stage progress to TUI (guard against nil channel)
			if req.EventChan != nil {
				agui.EmitStageProgress(req.EventChan, stageName, stageIdx, totalStages)
				agui.EmitThinking(req.EventChan, "code-owner", stageName+"...")
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

// runAdaptiveLoop runs a single agent node in a loop, accumulating context
// between iterations. The loop terminates when:
//   - the LLM produces zero tool calls (task naturally completed)
//   - MaxIterations is reached
//   - an error occurs
//
// This replaces the fixed multi-stage pipeline with dynamic, task-driven
// expansion: simple tasks finish in 1-2 iterations while complex tasks
// can use the full budget.
func (t *tree) runAdaptiveLoop(ctx context.Context, req TreeRequest) (TreeResult, error) {
	maxIter := t.config.MaxIterations
	logr := logger.GetLogger(ctx).With("fn", "tree.RunAdaptiveLoop", "goal", req.Goal, "maxIterations", maxIter)
	logr.Info("starting adaptive-loop ReAcTree execution")

	var accumulatedContext strings.Builder
	var lastOutput string
	var lastStatus NodeStatus
	iterationCount := 0

	for i := 0; i < maxIter; i++ {
		iterationCount = i + 1

		// Emit iteration progress to TUI
		if req.EventChan != nil {
			agui.EmitStageProgress(req.EventChan, fmt.Sprintf("Iteration %d", iterationCount), i, maxIter)
			agui.EmitThinking(req.EventChan, "code-owner", fmt.Sprintf("Thinking (iteration %d/%d)...", iterationCount, maxIter))
		}

		logr.Info("adaptive loop iteration started", "iteration", iterationCount)

		// Build a fresh single-node graph for this iteration.
		// Each iteration gets the full goal plus accumulated context.
		schema := NewReAcTreeSchema()
		sg := graph.NewStateGraph(schema)

		var capturedOutput string
		var capturedStatus NodeStatus
		var capturedTaskCompleted bool

		innerFunc := NewAgentNodeFunc(AgentNodeConfig{
			Goal:          req.Goal,
			Expert:        t.expert,
			WorkingMemory: t.workingMemory,
			Episodic:      t.episodic,
			MaxDecisions:  t.config.MaxDecisionsPerNode,
			EventChan:     req.EventChan,
			Tools:         req.Tools,
			SenderContext: req.SenderContext,
		})

		wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
			result, err := innerFunc(ctx, state)
			if err != nil {
				return result, err
			}
			if stateMap, ok := result.(graph.State); ok {
				if out, ok := stateMap[StateKeyOutput].(string); ok {
					capturedOutput = out
				}
				if status, ok := stateMap[StateKeyNodeStatus].(NodeStatus); ok {
					capturedStatus = status
				}
				if completed, ok := stateMap[StateKeyTaskCompleted].(bool); ok {
					capturedTaskCompleted = completed
				}
			}
			return result, nil
		}

		sg.AddNode("agent", wrappedFunc).
			SetEntryPoint("agent").
			SetFinishPoint("agent")

		compiled, err := sg.Compile()
		if err != nil {
			return TreeResult{Status: Failure}, fmt.Errorf("adaptive loop: failed to compile graph at iteration %d: %w", iterationCount, err)
		}

		executor, err := graph.NewExecutor(compiled,
			graph.WithMaxSteps(t.config.MaxTotalNodes),
		)
		if err != nil {
			return TreeResult{Status: Failure}, fmt.Errorf("adaptive loop: failed to create executor at iteration %d: %w", iterationCount, err)
		}

		initialState := graph.State{
			StateKeyGoal:             req.Goal,
			StateKeyIterationContext: accumulatedContext.String(),
			StateKeyIterationCount:   i,
		}

		events, err := executor.Execute(ctx, initialState, agent.NewInvocation())
		if err != nil {
			return TreeResult{Status: Failure}, fmt.Errorf("adaptive loop: execution failed at iteration %d: %w", iterationCount, err)
		}
		for range events {
			// Drain graph lifecycle events
		}

		lastOutput = capturedOutput
		lastStatus = capturedStatus

		// Accumulate this iteration's output for future iterations.
		if capturedOutput != "" {
			accumulatedContext.WriteString(fmt.Sprintf("\n--- Iteration %d ---\n", iterationCount))
			accumulatedContext.WriteString(capturedOutput)
		}

		logr.Info("adaptive loop iteration completed",
			"iteration", iterationCount,
			"status", capturedStatus,
			"task_completed", capturedTaskCompleted,
			"output_length", len(capturedOutput),
		)

		// Check termination conditions:
		// 1. Task completed (zero tool calls) → done
		if capturedTaskCompleted {
			logr.Info("adaptive loop: task completed naturally", "iterations", iterationCount)
			break
		}
		// 2. Failure → stop
		if capturedStatus == Failure {
			logr.Warn("adaptive loop: iteration failed, stopping", "iteration", iterationCount)
			break
		}
		// 3. Otherwise → continue to next iteration
	}

	result := TreeResult{
		Status:    lastStatus,
		Output:    lastOutput,
		NodeCount: iterationCount,
	}

	logr.Info("adaptive-loop ReAcTree execution completed",
		"status", result.Status,
		"iterations", iterationCount,
		"maxIterations", maxIter,
		"output_length", len(result.Output),
	)
	return result, nil
}
