package reactree

import (
	"context"

	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// ControlFlowType identifies the behavior of a control flow pattern.
type ControlFlowType string

const (
	// ControlFlowSequence executes children in order.
	// Returns Success only if ALL children succeed (AND logic).
	ControlFlowSequence ControlFlowType = "sequence"

	// ControlFlowFallback tries children in order.
	// Returns Success on the FIRST child success (OR logic).
	ControlFlowFallback ControlFlowType = "fallback"

	// ControlFlowParallel executes all children.
	// Returns Success if a majority succeed (majority vote).
	ControlFlowParallel ControlFlowType = "parallel"
)

// BuildSequence wires nodes into a sequence on the given StateGraph.
// Nodes are connected with AddEdge in order. After each node, a conditional
// edge checks NodeStatus: if Failure, the graph ends immediately.
// This models Behavior Tree Sequence (AND) semantics.
func BuildSequence(
	sg *graph.StateGraph,
	nodeIDs []string,
) *graph.StateGraph {
	if len(nodeIDs) == 0 {
		return sg
	}

	for i := 0; i < len(nodeIDs)-1; i++ {
		current := nodeIDs[i]
		next := nodeIDs[i+1]

		// After each node, check status: if success → next, if failure → end
		sg.AddConditionalEdges(current, statusRouter, map[string]string{
			"success": next,
			"failure": graph.End,
		})
	}

	// Last node goes to end on success
	last := nodeIDs[len(nodeIDs)-1]
	sg.SetFinishPoint(last)

	return sg
}

// BuildFallback wires nodes into a fallback on the given StateGraph.
// Nodes are connected so that if the current one succeeds the graph ends,
// otherwise the next node is tried. If all nodes fail, the graph ends with failure.
// This models Behavior Tree Fallback (OR) semantics.
func BuildFallback(
	sg *graph.StateGraph,
	nodeIDs []string,
) *graph.StateGraph {
	if len(nodeIDs) == 0 {
		return sg
	}

	for i := 0; i < len(nodeIDs)-1; i++ {
		current := nodeIDs[i]
		next := nodeIDs[i+1]

		// After each node: if success → end, if failure → try next
		sg.AddConditionalEdges(current, statusRouter, map[string]string{
			"success": graph.End,
			"failure": next,
		})
	}

	// Last node always goes to end
	last := nodeIDs[len(nodeIDs)-1]
	sg.SetFinishPoint(last)

	return sg
}

// BuildParallel wires nodes for parallel execution with majority voting.
// Callers are responsible for connecting the entry point (e.g., graph.Start)
// to each child in nodeIDs to create the fan-out. This function only wires
// the fan-in via AddJoinEdge to an aggregator node that performs majority voting.
func BuildParallel(
	sg *graph.StateGraph,
	nodeIDs []string,
	aggregatorID string,
) *graph.StateGraph {
	if len(nodeIDs) == 0 {
		return sg
	}

	// Add aggregator node that performs majority voting
	sg.AddNode(aggregatorID, majorityVoteFunc(nodeIDs))

	// Fan-in: all nodes must complete before the aggregator runs
	sg.AddJoinEdge(nodeIDs, aggregatorID)

	// Aggregator leads to end
	sg.SetFinishPoint(aggregatorID)

	return sg
}

// statusRouter is a conditional edge function that routes based on
// the NodeStatus stored in graph state. It returns "success" or "failure"
// as branch keys for the conditional edge path map.
func statusRouter(_ context.Context, state graph.State) (string, error) {
	status, ok := graph.GetStateValue[NodeStatus](state, StateKeyNodeStatus)
	if !ok {
		return "failure", nil
	}
	if status == Success {
		return "success", nil
	}
	return "failure", nil
}

// majorityVoteFunc creates a graph.NodeFunc that reads status results
// from each node and returns Success if a strict majority succeeded.
func majorityVoteFunc(nodeIDs []string) graph.NodeFunc {
	return func(ctx context.Context, state graph.State) (any, error) {
		logr := logger.GetLogger(ctx).With("fn", "majorityVoteFunc")

		// Read per-node results from state using node-specific status keys
		successes := 0
		for _, id := range nodeIDs {
			statusKey := StateKeyNodeStatus + ":" + id
			if status, ok := graph.GetStateValue[NodeStatus](state, statusKey); ok && status == Success {
				successes++
			}
		}

		total := len(nodeIDs)
		result := Failure
		if successes > total/2 {
			result = Success
		}

		logr.Debug("majority vote completed", "successes", successes, "total", total, "result", result)

		return graph.State{
			StateKeyNodeStatus: result,
		}, nil
	}
}
