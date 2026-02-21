package reactree

import (
	"fmt"
	"reflect"

	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// NodeStatus represents the outcome of a node execution in the ReAcTree.
// This enum follows the standard Behavior Tree convention: Running, Success, or Failure.
type NodeStatus int

const (
	// Running indicates the node is still executing and needs more ticks.
	Running NodeStatus = iota
	// Success indicates the node completed its goal successfully.
	Success
	// Failure indicates the node failed to complete its goal.
	Failure
)

// String returns a human-readable representation of the NodeStatus.
func (s NodeStatus) String() string {
	switch s {
	case Running:
		return "running"
	case Success:
		return "success"
	case Failure:
		return "failure"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// NodeResult carries the outcome of a node's execution, including
// any output text produced and any error encountered.
type NodeResult struct {
	Status NodeStatus
	Output string
	Err    error
}

// State keys used by the ReAcTree graph nodes to share data.
const (
	// StateKeyGoal is the current task goal for agent nodes to work on.
	StateKeyGoal = "reactree_goal"
	// StateKeyNodeStatus stores the NodeStatus result from the last node.
	StateKeyNodeStatus = "reactree_node_status"
	// StateKeyOutput stores the text output from the last node.
	StateKeyOutput = "reactree_output"
	// StateKeyWorkingMemory stores shared observations across nodes.
	StateKeyWorkingMemory = "reactree_working_memory"
	// StateKeyTaskCompleted indicates the agent finished without making any
	// tool calls. When true, the task was fully answered in this stage and
	// subsequent stages can be skipped to save cost and latency.
	StateKeyTaskCompleted = "reactree_task_completed"
	// StateKeyPreviousStageOutput carries the output from the previous stage
	// so subsequent stages can see what was already accomplished and avoid
	// redundant work (e.g. repeating the same web search).
	StateKeyPreviousStageOutput = "reactree_previous_stage_output"
	// StateKeyIterationContext carries the accumulated output from all prior
	// iterations in the adaptive loop. This replaces staged previousStageOutput
	// with a rolling context window.
	StateKeyIterationContext = "reactree_iteration_context"
	// StateKeyIterationCount tracks the current iteration number in the
	// adaptive loop (0-indexed).
	StateKeyIterationCount = "reactree_iteration_count"
	// StateKeyToolCallCounts reports per-tool call counts (name→count) from
	// a single agent node execution. The adaptive loop accumulates these
	// across iterations to enforce ToolBudgets.
	StateKeyToolCallCounts = "reactree_tool_call_counts"
)

// NewReAcTreeSchema creates a graph.StateSchema with the fields used by
// ReAcTree nodes. This schema defines the shared state that flows between
// nodes in the compiled graph.
func NewReAcTreeSchema() *graph.StateSchema {
	return graph.NewStateSchema().
		AddField(StateKeyGoal, graph.StateField{
			Type:    reflect.TypeOf(""),
			Reducer: graph.DefaultReducer,
		}).
		AddField(StateKeyNodeStatus, graph.StateField{
			Type:    reflect.TypeOf(Success),
			Reducer: graph.DefaultReducer,
			Default: func() any { return Running },
		}).
		AddField(StateKeyOutput, graph.StateField{
			Type:    reflect.TypeOf(""),
			Reducer: graph.DefaultReducer,
		}).
		AddField(StateKeyWorkingMemory, graph.StateField{
			Type:    reflect.TypeOf(map[string]any{}),
			Reducer: graph.MergeReducer,
			Default: func() any { return map[string]any{} },
		}).
		AddField(StateKeyTaskCompleted, graph.StateField{
			Type:    reflect.TypeOf(false),
			Reducer: graph.DefaultReducer,
			Default: func() any { return false },
		}).
		AddField(StateKeyPreviousStageOutput, graph.StateField{
			Type:    reflect.TypeOf(""),
			Reducer: graph.DefaultReducer,
			Default: func() any { return "" },
		}).
		AddField(StateKeyIterationContext, graph.StateField{
			Type:    reflect.TypeOf(""),
			Reducer: graph.DefaultReducer,
			Default: func() any { return "" },
		}).
		AddField(StateKeyIterationCount, graph.StateField{
			Type:    reflect.TypeOf(0),
			Reducer: graph.DefaultReducer,
			Default: func() any { return 0 },
		}).
		AddField(StateKeyToolCallCounts, graph.StateField{
			Type:    reflect.TypeOf(map[string]int{}),
			Reducer: graph.DefaultReducer,
			Default: func() any { return map[string]int{} },
		})
}
