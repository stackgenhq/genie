package reactree

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TreeConfig holds configuration for a ReAcTree execution run.
// These limits prevent runaway tree growth and unbounded LLM calls.
type TreeConfig struct {
	// MaxDepth limits how deep the tree can grow through recursive expansion.
	MaxDepth int

	// MaxDecisionsPerNode limits how many LLM calls a single agent node can make.
	MaxDecisionsPerNode int

	// MaxTotalNodes limits the total number of nodes in the tree.
	MaxTotalNodes int
}

// DefaultTreeConfig returns sensible defaults for tree execution.
func DefaultTreeConfig() TreeConfig {
	return TreeConfig{
		MaxDepth:            3,
		MaxDecisionsPerNode: 10,
		MaxTotalNodes:       20,
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
	Goal      string
	EventChan chan<- interface{}
	Tools     []tool.Tool
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

// Run builds a single-node StateGraph for the goal, compiles and executes it.
// This is the primary entry point for ReAcTree execution, delegating entirely
// to trpc-agent-go's graph.Executor.
func (t *tree) Run(ctx context.Context, req TreeRequest) (TreeResult, error) {
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
		NodeCount: 1, // Single root node for now
	}

	logr.Info("ReAcTree execution completed", "status", result.Status, "output_length", len(result.Output))
	return result, nil
}
