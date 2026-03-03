package reactree

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// Plan represents a decomposed task with subgoals and a control flow strategy.
//
// This implements the paper's "Expand" action (Algorithm 1, lines 20-28):
//
//	a_t^n = (f^n, [g_1^n, ..., g_K^n])
//
// where f^n is the control flow type (Flow) and each g_i^n is a natural language
// subgoal (Steps). A control flow node n_f with type f^n is attached as a child
// of the expanding agent node, and agent nodes n_i with subgoals g_i^n are added
// as children of n_f.
//
// Reference: ReAcTree (arXiv:2511.02424), Section 4.1, "Agent Nodes > Expanding"
type Plan struct {
	// Flow corresponds to f^n in the paper — the control flow type that
	// governs how child agent nodes are coordinated.
	//
	// Paper (Section 4.1, "Control Flow Nodes"):
	//   sequence (→) — returns success only if ALL children succeed
	//   fallback (?)  — returns success on the FIRST child success
	//   parallel (⇒) — aggregates outcomes via majority voting
	Flow ControlFlowType `json:"flow"`

	// Steps correspond to the subgoals [g_1^n, ..., g_K^n] in the paper.
	// Each step becomes an independent agent node n_i with its own goal,
	// tool set, and local context.
	Steps []PlanStep `json:"steps"`
}

// PlanStep is a single subgoal in a Plan, corresponding to one agent node n_i
// in the paper's tree. Each agent node operates as an LLM-based task planner
// with its own subgoal g_i^n, context c_t^n, and action space A_t^n.
//
// Reference: ReAcTree (arXiv:2511.02424), Section 4.1, "Agent Nodes"
type PlanStep struct {
	// Name uniquely identifies this step (used as graph node ID).
	Name string `json:"name"`

	// Goal is g_i^n — the natural language subgoal for this agent node.
	// The paper emphasizes that isolating subgoals reduces hallucination
	// and logical errors by keeping each agent focused on its local context.
	Goal string `json:"goal"`

	// Context is injected into the subgoal instruction.
	// Primarily used when a parent agent delegates a comparison task
	// and needs to provide historical states.
	Context string `json:"context,omitempty"`

	// Tools define the executable skill set A_t^n available to this node.
	// send_message is always stripped (framework invariant).
	Tools []string `json:"tools,omitempty"`

	// TaskType selects the LLM model for this step.
	TaskType modelprovider.TaskType `json:"task_type,omitempty"`
}

// OrchestratorConfig holds the dependencies for executing a Plan.
// This is the Go implementation of the inputs to Algorithm 2 (ExecCtrlFlowNode).
//
// Reference: ReAcTree (arXiv:2511.02424), Section 4.1, Algorithm 2
type OrchestratorConfig struct {
	// Expert provides the LLM policy p_LLM(·) used by each agent node
	// to sample actions. All nodes in the plan share this expert.
	// Thread safety: Expert.Do() creates independent runners per call,
	// so concurrent use from parallel nodes is safe.
	Expert expert.Expert

	// WorkingMemory is the shared blackboard (Section 4.2) that stores
	// environment-specific observations across all agent nodes in the tree.
	// Enables steps to share results without re-exploration.
	WorkingMemory *memory.WorkingMemory

	// Episodic is the experience store (Section 4.2) that records
	// subgoal-level trajectories. Successful step results are stored
	// as episodes for future in-context retrieval.
	Episodic memory.EpisodicMemory

	// MaxDecisions caps the number of LLM calls per agent node (D_max
	// in Algorithm 1, line 11). Prevents runaway iteration.
	MaxDecisions int

	ToolRegistry *tools.Registry

	// ToolWrapSvc wraps plan step tools with HITL approval, audit logging,
	// and caching — same as single sub-agent tools. Without this, plan step
	// agents would bypass human approval for write tools.
	ToolWrapSvc *toolwrap.Service

	// WrapRequest carries per-request fields (ThreadID, RunID)
	// needed for HITL wrapping to propagate approval events to the UI.
	WrapRequest toolwrap.WrapRequest

	// Timeout is the wall-clock deadline for the entire plan execution.
	// Prevents stuck plan steps from hanging the parent agent indefinitely.
	// Defaults to 3 minutes if zero.
	Timeout time.Duration

	// Toggles holds opt-in configurations for predictability.
	Toggles Toggles

	// ModelProvider resolves models for lightweight plan-step agents.
	// When set (along with ToolRegistry), plan-step agents use a minimal
	// sub-agent instruction instead of the full Expert persona.
	ModelProvider modelprovider.ModelProvider
}

// OrchestratorResult captures the outcome of a planned execution.
// Status maps to the success/failure return of Algorithm 2.
// Outputs maps each step name to its text result.
type OrchestratorResult struct {
	Status  NodeStatus
	Outputs map[string]string // step name → output
}

// ExecutePlan runs a Plan by building a graph with the appropriate control flow
// and agent nodes. This is the Go implementation of Algorithm 2 (ExecCtrlFlowNode).
//
// For "sequence" flow: steps run in order, sharing state. Each step's output
// becomes the previous stage output for the next step.
//
// For "parallel" flow: steps run concurrently via graph.AddJoinEdge. Results
// are aggregated by majority vote.
//
// For "fallback" flow: steps are tried in order. First success returns.
func ExecutePlan(ctx context.Context, plan Plan, cfg OrchestratorConfig) (OrchestratorResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "ExecutePlan", "flow", string(plan.Flow), "steps", len(plan.Steps))
	logr.Info("orchestrator executing plan")

	if len(plan.Steps) == 0 {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("plan has no steps")
	}

	// Apply timeout to prevent indefinite hangs.
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Single step: skip graph overhead, run directly.
	if len(plan.Steps) == 1 {
		return executeSingleStep(ctx, plan.Steps[0], cfg)
	}

	// Build graph with control flow.
	schema := NewReAcTreeSchema()
	sg := graph.NewStateGraph(schema)

	// Collect outputs from each node via closures.
	var mu sync.Mutex
	outputs := make(map[string]string, len(plan.Steps))

	nodeIDs := make([]string, len(plan.Steps))
	for i, step := range plan.Steps {
		nodeIDs[i] = step.Name
		stepCopy := step // capture for closure

		// Scope tools to only what this step needs.
		// If the step specifies tools, use Include to filter.
		// Otherwise, all tools are available.
		tools := cfg.ToolRegistry.Include(stepCopy.Tools...).AllTools()

		// 1. Enterprise Bounding: wrap tools with middleware if enabled.
		if cfg.Toggles.EnableCriticMiddleware {
			validator := NewDeterministicValidator(nil) // Block none as default for now
			for i, tl := range tools {
				tools[i] = WrapWithValidator(tl, validator)
			}
		}

		// 2. Wrap tools with HITL approval, audit logging, and caching.
		// This ensures plan step agents go through the same approval gate
		// as single sub-agent tools (fixes HITL bypass bug).
		if cfg.ToolWrapSvc != nil {
			tools = cfg.ToolWrapSvc.Wrap(tools, cfg.WrapRequest)
		}

		// Working memory is injected into the prompt automatically
		// (read) and stored back after agent completion (write).
		// No scratchpad tools needed — follows trpc-agent-go pattern.

		// Build a minimal sub-agent instruction from the tool names.
		// This prevents plan-step agents from inheriting the full
		// codeowner persona (which causes tool hallucination).
		var toolNames []string
		for _, tl := range tools {
			toolNames = append(toolNames, tl.Declaration().Name)
		}
		subAgentInstruction := buildSubAgentInstruction(toolNames)

		prompt := stepCopy.Goal
		if stepCopy.Context != "" {
			prompt = fmt.Sprintf("Context:\n%s\n\nGoal:\n%s", stepCopy.Context, stepCopy.Goal)
		}

		agentFunc := NewAgentNodeFunc(AgentNodeConfig{
			Goal:              prompt,
			Expert:            cfg.Expert,
			WorkingMemory:     cfg.WorkingMemory,
			Episodic:          cfg.Episodic,
			MaxDecisions:      cfg.MaxDecisions,
			Tools:             tools,
			TaskType:          stepCopy.TaskType,
			SystemInstruction: subAgentInstruction,
			ModelProvider:     cfg.ModelProvider,
		})

		// Wrap to capture output per-step.
		wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
			result, err := agentFunc(ctx, state)
			if err != nil {
				// Emit a failure progress message for this step.
				agui.Emit(ctx, agui.AgentToolResponseMsg{
					ToolName: "plan_step_progress",
					Response: fmt.Sprintf("❌ %s failed: %v", stepCopy.Name, err),
				})
				return result, err
			}
			if stateMap, ok := result.(graph.State); ok {
				// Check node status — only store successful results.
				nodeStatus := Success
				if s, ok := stateMap[StateKeyNodeStatus].(NodeStatus); ok {
					nodeStatus = s
				}

				if out, ok := stateMap[StateKeyOutput].(string); ok && out != "" {
					mu.Lock()
					outputs[stepCopy.Name] = out
					mu.Unlock()

					// Store in working memory for other steps to access.
					if cfg.WorkingMemory != nil {
						cfg.WorkingMemory.Store(
							fmt.Sprintf("plan_step:%s:result", stepCopy.Name),
							out,
						)
					}

					// Store as episode for future in-context retrieval.
					// Only store successful episodes to avoid polluting
					// memory with failed trajectories labeled as successes.
					if cfg.Episodic != nil && nodeStatus == Success {
						trajectory := out
						const maxTraj = 500
						if len(trajectory) > maxTraj {
							trajectory = trajectory[:maxTraj] + "... (truncated)"
						}
						cfg.Episodic.Store(ctx, memory.Episode{
							Goal:       stepCopy.Goal,
							Trajectory: trajectory,
							Status:     memory.EpisodeSuccess,
						})
					}

					// Emit a human-friendly progress message for this step.
					// Build a short tweet-like summary.
					tweet := fmt.Sprintf("✅ **%s** completed — gathered %d chars of findings.", stepCopy.Name, len(out))
					agui.Emit(ctx, agui.AgentToolResponseMsg{
						ToolName: "plan_step_progress",
						Response: tweet,
					})
				} else {
					// Step completed but produced no output.
					agui.Emit(ctx, agui.AgentToolResponseMsg{
						ToolName: "plan_step_progress",
						Response: fmt.Sprintf("⚠️ **%s** finished but did not produce output.", stepCopy.Name),
					})
				}
			}
			return result, nil
		}

		if plan.Flow == ControlFlowParallel {
			sg.AddNode(stepCopy.Name, WrapNodeForParallel(stepCopy.Name, wrappedFunc))
		} else {
			sg.AddNode(stepCopy.Name, wrappedFunc)
		}
	}

	// Wire control flow — each flow type manages its own entry/finish points.
	switch plan.Flow {
	case ControlFlowSequence:
		sg.SetEntryPoint(nodeIDs[0])
		BuildSequenceWithEarlyExit(sg, nodeIDs)
	case ControlFlowParallel:
		aggregatorID := "plan_aggregator"
		// The graph library requires SetEntryPoint for Compile() validation.
		// Set the first node as entry; BuildParallel wires all nodes via
		// AddEdge(Start, ...) internally for actual fan-out execution.
		sg.SetEntryPoint(nodeIDs[0])
		for _, id := range nodeIDs {
			sg.AddEdge(graph.Start, id)
		}
		BuildParallel(sg, nodeIDs, aggregatorID)
	case ControlFlowFallback:
		sg.SetEntryPoint(nodeIDs[0])
		BuildFallback(sg, nodeIDs)
	default:
		sg.SetEntryPoint(nodeIDs[0])
		BuildSequenceWithEarlyExit(sg, nodeIDs)
	}

	compiled, err := sg.Compile()
	if err != nil {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("failed to compile plan graph: %w", err)
	}

	executor, err := graph.NewExecutor(compiled, graph.WithMaxSteps(len(plan.Steps)*3))
	if err != nil {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("failed to create plan executor: %w", err)
	}

	initialState := graph.State{
		StateKeyGoal: joinStepGoals(plan.Steps),
	}

	events, err := executor.Execute(ctx, initialState, agent.NewInvocation())
	if err != nil {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("plan execution failed: %w", err)
	}
	for range events {
		// Drain graph lifecycle events.
	}

	// Determine overall status from the final graph state.
	status := Success
	if len(outputs) == 0 {
		status = Failure
	}

	logr.Info("plan execution completed", "status", status, "steps_with_output", len(outputs))

	return OrchestratorResult{
		Status:  status,
		Outputs: outputs,
	}, nil
}

// executeSingleStep runs a single step without graph overhead.
func executeSingleStep(ctx context.Context, step PlanStep, cfg OrchestratorConfig) (OrchestratorResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "executeSingleStep", "step", step.Name)
	logr.Info("executing single plan step")

	schema := NewReAcTreeSchema()
	sg := graph.NewStateGraph(schema)

	var capturedOutput string
	var capturedStatus NodeStatus

	toolsToUse := cfg.ToolRegistry.AllTools()

	// Enterprise Bounding: wrap tools with middleware if enabled.
	if cfg.Toggles.EnableCriticMiddleware {
		validator := NewDeterministicValidator(nil) // Block none as default for now
		for i, tl := range toolsToUse {
			toolsToUse[i] = WrapWithValidator(tl, validator)
		}
	}

	// Working memory is injected into the prompt automatically
	// (read) and stored back after agent completion (write).
	// No scratchpad tools needed — follows trpc-agent-go pattern.

	// Build a minimal sub-agent instruction.
	var toolNames []string
	for _, tl := range toolsToUse {
		toolNames = append(toolNames, tl.Declaration().Name)
	}
	subAgentInstruction := buildSubAgentInstruction(toolNames)

	prompt := step.Goal
	if step.Context != "" {
		prompt = fmt.Sprintf("Context:\n%s\n\nGoal:\n%s", step.Context, step.Goal)
	}

	agentFunc := NewAgentNodeFunc(AgentNodeConfig{
		Goal:              prompt,
		Expert:            cfg.Expert,
		WorkingMemory:     cfg.WorkingMemory,
		Episodic:          cfg.Episodic,
		MaxDecisions:      cfg.MaxDecisions,
		Tools:             toolsToUse,
		TaskType:          step.TaskType,
		SystemInstruction: subAgentInstruction,
		ModelProvider:     cfg.ModelProvider,
	})

	wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
		result, err := agentFunc(ctx, state)
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
		}
		return result, nil
	}

	sg.AddNode(step.Name, wrappedFunc).
		SetEntryPoint(step.Name).
		SetFinishPoint(step.Name)

	compiled, err := sg.Compile()
	if err != nil {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("failed to compile single-step graph: %w", err)
	}

	executor, err := graph.NewExecutor(compiled, graph.WithMaxSteps(3))
	if err != nil {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("failed to create single-step executor: %w", err)
	}

	initialState := graph.State{
		StateKeyGoal: step.Goal,
	}

	events, err := executor.Execute(ctx, initialState, agent.NewInvocation())
	if err != nil {
		return OrchestratorResult{Status: Failure}, fmt.Errorf("single-step execution failed: %w", err)
	}
	for range events {
	}

	// Store in working memory.
	if cfg.WorkingMemory != nil && capturedOutput != "" {
		cfg.WorkingMemory.Store(
			fmt.Sprintf("plan_step:%s:result", step.Name),
			capturedOutput,
		)
	}

	// Store as episode for future in-context retrieval — only for successful steps.
	if cfg.Episodic != nil && capturedOutput != "" && capturedStatus == Success {
		trajectory := capturedOutput
		const maxTraj = 500
		if len(trajectory) > maxTraj {
			trajectory = trajectory[:maxTraj] + "... (truncated)"
		}
		cfg.Episodic.Store(ctx, memory.Episode{
			Goal:       step.Goal,
			Trajectory: trajectory,
			Status:     memory.EpisodeSuccess,
		})
	}

	return OrchestratorResult{
		Status: capturedStatus,
		Outputs: map[string]string{
			step.Name: capturedOutput,
		},
	}, nil
}

// joinStepGoals creates a composite goal string from all plan steps.
func joinStepGoals(steps []PlanStep) string {
	goals := make([]string, len(steps))
	for i, s := range steps {
		goals[i] = fmt.Sprintf("%d. %s: %s", i+1, s.Name, s.Goal)
	}
	return "Execute the following plan:\n" + strings.Join(goals, "\n")
}
