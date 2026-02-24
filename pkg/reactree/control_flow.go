package reactree

import (
	"context"

	"github.com/stackgenhq/genie/pkg/logger"
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

// BuildSequenceWithEarlyExit wires nodes into a sequence that supports
// early termination when a stage completes the task without tool calls.
// Like BuildSequence, it short-circuits on Failure. Additionally, if a
// stage sets StateKeyTaskCompleted=true (zero tool calls), remaining
// stages are skipped — preventing redundant cost and latency.
func BuildSequenceWithEarlyExit(
	sg *graph.StateGraph,
	nodeIDs []string,
) *graph.StateGraph {
	if len(nodeIDs) == 0 {
		return sg
	}

	for i := 0; i < len(nodeIDs)-1; i++ {
		current := nodeIDs[i]
		next := nodeIDs[i+1]

		// After each node: success → next, failure → end, completed → end (early exit)
		sg.AddConditionalEdges(current, stageRouter, map[string]string{
			"success":   next,
			"failure":   graph.End,
			"completed": graph.End,
		})
	}

	// Last node always goes to end
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
//
// Each child node's original function is wrapped so that its NodeStatus is
// also stored under a per-node key (StateKeyNodeStatus:<nodeID>). This lets
// the aggregator read individual results without them being overwritten by
// sibling nodes that share the same StateKeyNodeStatus key.
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

// WrapNodeForParallel wraps a node function so that, in addition to returning
// its normal state, the NodeStatus is also stored under a per-node key
// (StateKeyNodeStatus:<nodeID>). This allows the majority-vote aggregator to
// read each node's result independently.
//
// Usage: when building a parallel graph, wrap each child node before adding it:
//
//	sg.AddNode("a", WrapNodeForParallel("a", myNodeFunc))
//	sg.AddNode("b", WrapNodeForParallel("b", otherNodeFunc))
//	BuildParallel(sg, []string{"a", "b"}, "aggregator")
func WrapNodeForParallel(nodeID string, original graph.NodeFunc) graph.NodeFunc {
	return func(ctx context.Context, state graph.State) (any, error) {
		result, err := original(ctx, state)
		if err != nil {
			return result, err
		}

		// Copy the NodeStatus to a per-node key so the aggregator can read it.
		if stateMap, ok := result.(graph.State); ok {
			if status, exists := stateMap[StateKeyNodeStatus]; exists {
				stateMap[StateKeyNodeStatus+":"+nodeID] = status
			}
		}
		return result, nil
	}
}

// statusRouter is a conditional edge function that routes based on
// the NodeStatus stored in graph state. It returns "success" or "failure"
// as branch keys for the conditional edge path map.
func statusRouter(ctx context.Context, state graph.State) (string, error) {
	logr := logger.GetLogger(ctx).With("fn", "statusRouter")
	status, ok := graph.GetStateValue[NodeStatus](state, StateKeyNodeStatus)
	if !ok {
		logr.Warn("no node status found in state, defaulting to failure")
		return "failure", nil
	}
	if status == Success {
		logr.Debug("routing: success")
		return "success", nil
	}
	logr.Info("routing: failure", "status", status)
	return "failure", nil
}

// stageRouter is a conditional edge function for multi-stage sequences
// with early-exit support. It checks both NodeStatus and TaskCompleted:
//   - failure  → stage failed, abort pipeline
//   - completed → stage succeeded with zero tool calls, task is done
//   - success  → stage succeeded with tool calls, continue to next stage
func stageRouter(ctx context.Context, state graph.State) (string, error) {
	logr := logger.GetLogger(ctx).With("fn", "stageRouter")
	status, ok := graph.GetStateValue[NodeStatus](state, StateKeyNodeStatus)
	if !ok {
		logr.Warn("no node status found in state, defaulting to failure")
		return "failure", nil
	}
	if status != Success {
		logr.Info("stage routing: failure", "status", status)
		return "failure", nil
	}
	completed, _ := graph.GetStateValue[bool](state, StateKeyTaskCompleted)
	if completed {
		logr.Info("stage routing: early exit (task completed with zero tool calls)")
		return "completed", nil
	}
	logr.Debug("stage routing: success, continuing to next stage")
	return "success", nil
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

		logr.Info("majority vote completed", "successes", successes, "total", total, "result", result)

		return graph.State{
			StateKeyNodeStatus: result,
		}, nil
	}
}
