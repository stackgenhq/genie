package reactree

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// runAdaptiveLoop coordinates the high-level execution flow.
func (t *tree) runAdaptiveLoop_v2(ctx context.Context, req TreeRequest) (TreeResult, error) {
	ls := &loopState{maxIterations: t.config.MaxIterations}
	logr := logger.GetLogger(ctx).With("fn", "tree.RunAdaptiveLoop", "goal", req.Goal)

	// Create a parent span so all iteration spans are children of one Langfuse trace.
	ctx, parentSpan := trace.Tracer.Start(ctx, "reactree.adaptive_loop")
	parentSpan.SetAttributes(
		attribute.String("reactree.goal", req.Goal),
		attribute.Int("reactree.max_iterations", ls.maxIterations),
	)
	defer func() {
		parentSpan.SetAttributes(attribute.Int("reactree.iterations", ls.iteration))
		parentSpan.End()
	}()

	for i := 0; i < ls.maxIterations; i++ {
		ls.iteration = i + 1
		t.emitIterationProgress(ctx, req, ls)

		// 1. Execute the iteration logic
		err := t.executeIteration(ctx, req, ls)
		if err != nil {
			parentSpan.RecordError(err)
			parentSpan.SetStatus(codes.Error, err.Error())
			return TreeResult{Status: Failure}, err
		}

		// 2. Process results and update context
		t.updateLoopState(ls, req.EventChan)

		// 3. Check termination conditions
		if ls.capturedTaskCompleted {
			logr.Info("adaptive loop: task completed naturally", "iterations", ls.iteration)
			break
		}
		if ls.capturedStatus == Failure {
			logr.Warn("adaptive loop: iteration failed, stopping", "iteration", ls.iteration)
			break
		}
		if ls.checkRepetition() {
			logr.Warn("adaptive loop: stuck in repetition, breaking",
				"iteration", ls.iteration,
				"repeated_count", ls.repetitionCount,
			)
			ls.lastStatus = Failure
			ls.lastOutput = "I got stuck repeating the same approach. Please try rephrasing your request."
			break
		}
	}

	t.ensureUserFeedback(ctx, req, ls)
	return ls.toResult(), nil
}

// maxRepetitions is the number of consecutive identical outputs that triggers
// the repetition detector. Prevents the LLM from burning iterations when it
// gets stuck calling the same tool with the same arguments repeatedly.
const maxRepetitions = 3

// loopState tracks the internal state of the loop to keep function signatures clean.
type loopState struct {
	iteration             int
	maxIterations         int
	contextBuffer         strings.Builder
	lastOutput            string
	lastStatus            NodeStatus
	priorHadOutput        bool
	textWasStreamed       bool
	capturedOutput        string
	capturedStatus        NodeStatus
	capturedTaskCompleted bool

	// Repetition detection: tracks consecutive identical outputs to break
	// stuck loops where the LLM repeats the same failing action.
	lastOutputHash  uint64
	repetitionCount int
}

// executeIteration builds, compiles, and runs the graph for a single cycle.
func (t *tree) executeIteration(ctx context.Context, req TreeRequest, ls *loopState) error {
	compiled, err := t.prepareGraph(req, ls)
	if err != nil {
		return fmt.Errorf("failed to compile graph at iteration %d: %w", ls.iteration, err)
	}

	executor, err := graph.NewExecutor(compiled, graph.WithMaxSteps(t.config.MaxTotalNodes))
	if err != nil {
		return fmt.Errorf("failed to create executor at iteration %d: %w", ls.iteration, err)
	}

	initialState := graph.State{
		StateKeyGoal:             req.Goal,
		StateKeyIterationContext: ls.contextBuffer.String(),
		StateKeyIterationCount:   ls.iteration - 1,
	}

	events, err := executor.Execute(ctx, initialState, agent.NewInvocation())
	if err != nil {
		return fmt.Errorf("execution failed at iteration %d: %w", ls.iteration, err)
	}

	for range events { /* Drain graph lifecycle events */
	}
	return nil
}

// prepareGraph isolates the node creation and state extraction logic.
func (t *tree) prepareGraph(req TreeRequest, ls *loopState) (*graph.Graph, error) {
	schema := NewReAcTreeSchema()
	sg := graph.NewStateGraph(schema)

	// Always forward EventChan so the user sees tool-call progress indicators
	// ("Thinking...") during every iteration, including validation probes.
	// Output suppression is handled at the result level by updateLoopState.
	iterEventChan := req.EventChan

	innerFunc := NewAgentNodeFunc(AgentNodeConfig{
		Goal:          req.Goal,
		Expert:        t.expert,
		WorkingMemory: t.workingMemory,
		Episodic:      t.episodic,
		MaxDecisions:  t.config.MaxDecisionsPerNode,
		EventChan:     iterEventChan,
		Tools:         req.Tools,
		SenderContext: req.SenderContext,
		TaskType:      req.TaskType,
	})

	wrappedFunc := func(ctx context.Context, state graph.State) (any, error) {
		result, err := innerFunc(ctx, state)
		if err != nil {
			return result, err
		}
		// Reset captured state to prevent stale values and extract from result.
		ls.capturedOutput, ls.capturedStatus, ls.capturedTaskCompleted = "", 0, false
		if stateMap, ok := result.(graph.State); ok {
			if val, ok := stateMap[StateKeyOutput].(string); ok {
				ls.capturedOutput = val
			}
			if val, ok := stateMap[StateKeyNodeStatus].(NodeStatus); ok {
				ls.capturedStatus = val
			}
			if val, ok := stateMap[StateKeyTaskCompleted].(bool); ok {
				ls.capturedTaskCompleted = val
			}
		}
		return result, nil
	}

	sg.AddNode("agent", wrappedFunc).SetEntryPoint("agent").SetFinishPoint("agent")
	return sg.Compile()
}

// updateLoopState handles logic for output suppression and context accumulation.
func (t *tree) updateLoopState(ls *loopState, eventChan chan<- any) {
	// If task is complete but we already had output, suppress the "validation probe" chatter
	if !ls.capturedTaskCompleted || !ls.priorHadOutput {
		ls.lastOutput = ls.capturedOutput
		ls.lastStatus = ls.capturedStatus
	}

	// Since EventChan is always forwarded, mark text as streamed whenever
	// the completing iteration produced output and was sent to the user.
	if eventChan != nil && ls.capturedTaskCompleted && ls.capturedOutput != "" {
		ls.textWasStreamed = true
	}

	if ls.capturedTaskCompleted && ls.capturedOutput != "" {
		ls.priorHadOutput = true
	}

	ls.accumulateContext()
}

// accumulateContext manages the context buffer with truncation logic.
func (ls *loopState) accumulateContext() {
	if ls.capturedOutput == "" {
		return
	}

	const maxContext = 4000
	newEntry := fmt.Sprintf("\n--- Iteration %d ---\n%s", ls.iteration, ls.capturedOutput)

	if ls.contextBuffer.Len()+len(newEntry) > maxContext {
		tail := ls.contextBuffer.String()
		if len(tail) > maxContext/2 {
			tail = tail[len(tail)-maxContext/2:]
		}
		ls.contextBuffer.Reset()
		ls.contextBuffer.WriteString("... (earlier iterations summarized)\n")
		ls.contextBuffer.WriteString(tail)
	}
	ls.contextBuffer.WriteString(newEntry)
}

// checkRepetition detects when the adaptive loop is stuck producing the same
// output repeatedly. This prevents the LLM from burning through MaxIterations
// (and API credits) when it gets stuck retrying the same failing tool call.
// Returns true when the repetition threshold is reached.
func (ls *loopState) checkRepetition() bool {
	if ls.capturedOutput == "" {
		return false
	}

	h := fnv.New64a()
	h.Write([]byte(ls.capturedOutput))
	hash := h.Sum64()

	if hash == ls.lastOutputHash {
		ls.repetitionCount++
	} else {
		ls.lastOutputHash = hash
		ls.repetitionCount = 1
	}

	return ls.repetitionCount >= maxRepetitions
}

// ensureUserFeedback provides fallback UI messages if the loop was silent.
func (t *tree) ensureUserFeedback(ctx context.Context, req TreeRequest, ls *loopState) {
	if ls.textWasStreamed || req.EventChan == nil {
		return
	}

	switch {
	case ls.lastOutput != "":
		agui.EmitAgentMessage(ctx, req.EventChan, "genie", ls.lastOutput)
	case ls.contextBuffer.Len() > 0:
		agui.EmitAgentMessage(ctx, req.EventChan, "genie", "I ran into issues but found this:\n\n"+ls.contextBuffer.String())
	default:
		agui.EmitAgentMessage(ctx, req.EventChan, "genie", "I encountered an issue and couldn't complete this request.")
	}
}

func (ls *loopState) toResult() TreeResult {
	return TreeResult{
		Status:    ls.lastStatus,
		Output:    ls.lastOutput,
		NodeCount: ls.iteration,
	}
}

func (t *tree) emitIterationProgress(ctx context.Context, req TreeRequest, ls *loopState) {
	if req.EventChan == nil {
		return
	}
	agui.EmitStageProgress(ctx, req.EventChan, fmt.Sprintf("Iteration %d", ls.iteration), ls.iteration-1, ls.maxIterations)
	agui.EmitThinking(ctx, req.EventChan, "code-owner", fmt.Sprintf("Thinking (%d/%d)...", ls.iteration, ls.maxIterations))
}
