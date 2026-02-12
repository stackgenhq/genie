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
		})
}
