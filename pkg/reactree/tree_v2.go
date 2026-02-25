package reactree

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// runAdaptiveLoop coordinates the high-level execution flow.
func (t *tree) runAdaptiveLoop_v2(ctx context.Context, req TreeRequest) (TreeResult, error) {
	ls := &loopState{
		maxIterations:  t.config.MaxIterations,
		toolBudgets:    t.config.ToolBudgets,
		toolCallCounts: make(map[string]int),
	}
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

		// Hook: iteration start.
		t.hooks.OnIterationStart(ctx, hooks.IterationStartEvent{
			Goal:          req.Goal,
			Iteration:     ls.iteration,
			MaxIterations: ls.maxIterations,
		})

		// 1. Execute the iteration logic
		err := t.executeIteration(ctx, req, ls)
		if err != nil {
			parentSpan.RecordError(err)
			parentSpan.SetStatus(codes.Error, err.Error())
			return TreeResult{Status: Failure}, err
		}

		// 2. Process results and update context
		t.updateLoopState(ls)

		// Hook: iteration end.
		t.hooks.OnIterationEnd(ctx, hooks.IterationEndEvent{
			Iteration:      ls.iteration,
			Status:         ls.capturedStatus.String(),
			ToolCallCounts: ls.toolCallCounts,
			TaskCompleted:  ls.capturedTaskCompleted,
			Output:         ls.capturedOutput,
		})

		// Enterprise: run RAR reflection if enabled.
		if t.config.Toggles.EnableActionReflection {
			// Derive the list of tools actually called during this iteration.
			var toolsCalled []string
			for name, count := range ls.toolCallCounts {
				if count > 0 {
					toolsCalled = append(toolsCalled, name)
				}
			}

			reflResult, reflErr := t.reflector.Reflect(ctx, ReflectionRequest{
				Goal:           req.Goal,
				ProposedOutput: ls.capturedOutput,
				IterationCount: ls.iteration,
				ToolCallsMade:  toolsCalled,
			})
			if reflErr == nil {
				// Hook: reflection result.
				t.hooks.OnReflection(ctx, hooks.ReflectionEvent{
					Iteration:     ls.iteration,
					Monologue:     reflResult.Monologue,
					ShouldProceed: reflResult.ShouldProceed,
				})
				if !reflResult.ShouldProceed {
					logr.Warn("adaptive loop: reflection halted execution", "iteration", ls.iteration)
					ls.lastStatus = Failure
					ls.lastOutput = "Reflection review halted execution: " + reflResult.Monologue
					break
				}
			}
		}

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

	t.ensureUserFeedback(ctx, ls)
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
	toolBudgets           map[string]int // per-tool call limits (from TreeConfig)
	toolCallCounts        map[string]int // cumulative calls per tool across iterations
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

// toolsForIteration returns the tool list with budget-exceeded tools removed.
// This is a hard code-level guardrail — the LLM literally cannot call a tool
// that isn't in the list. Any tool listed in ToolBudgets whose cumulative count
// has reached its limit gets stripped.
func (ls *loopState) toolsForIteration(tools []tool.Tool) []tool.Tool {
	if len(ls.toolBudgets) == 0 {
		return tools
	}
	// Build set of tools that have exceeded their budget.
	exceeded := make(map[string]bool)
	for name, limit := range ls.toolBudgets {
		if ls.toolCallCounts[name] >= limit {
			exceeded[name] = true
		}
	}
	if len(exceeded) == 0 {
		return tools
	}
	filtered := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if !exceeded[t.Declaration().Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// budgetExhaustedTools returns the list of tool names that have exceeded their
// budget. Used to inject prompt hints telling the LLM not to attempt them.
func (ls *loopState) budgetExhaustedTools() []string {
	var exhausted []string
	for name, limit := range ls.toolBudgets {
		if ls.toolCallCounts[name] >= limit {
			exhausted = append(exhausted, name)
		}
	}
	return exhausted
}

// executeIteration builds, compiles, and runs the graph for a single cycle.
func (t *tree) executeIteration(ctx context.Context, req TreeRequest, ls *loopState) error {
	compiled, err := t.prepareGraph(req, ls)
	if err != nil {
		return fmt.Errorf("failed to compile graph at iteration %d: %w", ls.iteration, err)
	}

	opts := []graph.ExecutorOption{graph.WithMaxSteps(t.config.MaxTotalNodes)}
	if t.config.Checkpointer != nil {
		opts = append(opts, graph.WithCheckpointSaver(t.config.Checkpointer))
	}
	executor, err := graph.NewExecutor(compiled, opts...)
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

	toolsToUse := ls.toolsForIteration(req.Tools)

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
		Goal:                 req.Goal,
		Expert:               t.expert,
		WorkingMemory:        t.resolveWorkingMemory(req),
		Episodic:             t.resolveEpisodic(req),
		MaxDecisions:         t.config.MaxDecisionsPerNode,
		Tools:                toolsToUse,
		TaskType:             req.TaskType,
		Attachments:          req.Attachments,
		BudgetExhaustedTools: ls.budgetExhaustedTools(),
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
			// Accumulate per-tool call counts for budget enforcement.
			if counts, ok := stateMap[StateKeyToolCallCounts].(map[string]int); ok {
				for name, count := range counts {
					ls.toolCallCounts[name] += count
				}
			}
		}
		return result, nil
	}

	sg.AddNode("agent", wrappedFunc).SetEntryPoint("agent").SetFinishPoint("agent")
	return sg.Compile()
}

// updateLoopState handles logic for output suppression and context accumulation.
func (t *tree) updateLoopState(ls *loopState) {
	// If task is complete but we already had output, suppress the "validation probe" chatter
	if !ls.capturedTaskCompleted || !ls.priorHadOutput {
		ls.lastOutput = ls.capturedOutput
		ls.lastStatus = ls.capturedStatus
	}

	// Mark text as streamed when the completing iteration produced output.
	// The bus handles event routing — we just need to track that streaming happened.
	if ls.capturedTaskCompleted && ls.capturedOutput != "" {
		ls.textWasStreamed = true
	}

	if ls.capturedTaskCompleted && ls.capturedOutput != "" {
		ls.priorHadOutput = true
	}

	ls.accumulateContext()
}

// accumulateContext appends iteration output to the context buffer.
//
// Context compression is now handled by trpc-agent-go's SessionSummarizer
// (configured via inmemory.WithSummarizer in expert.go and create_agent.go)
// rather than manual character-level truncation. The context buffer here
// records iteration history for the adaptive loop's progress tracking only.
//
// Previously, this method applied a 4000-char hard cap with arbitrary
// midpoint splicing — the key gap identified in the StateLM/Pensieve analysis.
func (ls *loopState) accumulateContext() {
	if ls.capturedOutput == "" {
		return
	}

	// Large outputs are still capped per-iteration to prevent a single
	// verbose tool response from dominating the loop context. The
	// session-level summarizer handles cross-turn compression.
	const maxPerIteration = 3000
	output := ls.capturedOutput
	if len(output) > maxPerIteration {
		output = "... (earlier output truncated)\n" + output[len(output)-maxPerIteration:]
	}

	fmt.Fprintf(&ls.contextBuffer, "\n--- Iteration %d ---\n%s", ls.iteration, output)
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
func (t *tree) ensureUserFeedback(ctx context.Context, ls *loopState) {
	if ls.textWasStreamed || agui.ChannelFor(ctx) == nil {
		return
	}

	switch {
	case ls.lastOutput != "":
		agui.EmitAgentMessage(ctx, "genie", ls.lastOutput)
	case ls.contextBuffer.Len() > 0:
		agui.EmitAgentMessage(ctx, "genie", "I ran into issues but found this:\n\n"+ls.contextBuffer.String())
	default:
		agui.EmitAgentMessage(ctx, "genie", "I encountered an issue and couldn't complete this request.")
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
	if agui.ChannelFor(ctx) == nil {
		return
	}
	agui.EmitStageProgress(ctx, fmt.Sprintf("Iteration %d", ls.iteration), ls.iteration-1, ls.maxIterations)
	agui.EmitThinking(ctx, "orchestrator", fmt.Sprintf("Thinking (%d/%d)...", ls.iteration, ls.maxIterations))
}
