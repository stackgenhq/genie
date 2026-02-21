package reactree

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/agentutils"
	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/retrier"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	"go.opentelemetry.io/otel"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const summarizeThreshold = 2000

// CreateAgentRequest is the input for the create_agent tool.
type CreateAgentRequest struct {
	AgentName         string                 `json:"agent_name" jsonschema:"description=Name of the sub-agent,required"`
	Goal              string                 `json:"goal" jsonschema:"description=The goal or task for the sub-agent to accomplish,required"`
	ToolNames         []string               `json:"tool_names,omitempty" jsonschema:"description=Names of tools to give the sub-agent. If empty all tools are provided."`
	TaskType          modelprovider.TaskType `json:"task_type,omitempty" jsonschema:"description=Type of task for the sub-agent to accomplish, Should be one of efficiency/long_horizon_autonomy/mathematical/general_task/novel_reasoning/scientific_reasoning/terminal_calling/planning,required"`
	MaxToolIterations int                    `json:"max_tool_iterations,omitempty" jsonschema:"description=Maximum tool iterations. Scale to complexity: simple lookups 3-5 and file edits 10-15 and multi-step 15-25,required"`
	MaxLLMCalls       int                    `json:"max_llm_calls,omitempty" jsonschema:"description=Maximum LLM calls. Scale to complexity: simple lookups 3-5 and file edits 10-15 and multi-step 15-25,required"`

	// Steps enables multi-step plan execution. When provided, the tool builds
	// a graph from these steps using the specified Flow type, instead of
	// running a single sub-agent. Each step becomes an agent node.
	Steps []PlanStep `json:"steps,omitempty" jsonschema:"description=Optional list of sub-steps for multi-step plan execution. When provided the goal is decomposed into these steps coordinated by flow_type."`

	// Flow selects how Steps are coordinated:
	//   sequence  — steps run in order (fail-fast)
	//   parallel  — steps run concurrently (majority vote)
	//   fallback  — steps tried in order (first success wins)
	// Defaults to sequence if not specified.
	Flow string `json:"flow_type,omitempty" jsonschema:"description=How steps are coordinated: sequence (default) or parallel or fallback. Only used when steps is provided."`
}

// CreateAgentResponse is the output for the create_agent tool.
type CreateAgentResponse struct {
	Output string `json:"output"`
	Status string `json:"status"`
}

// createAgentTool dynamically creates a sub-agent with a selected subset of
// tools from a registry. Each invocation builds a fresh llmagent with only the
// requested tools, runs it, and returns the result.
type createAgentTool struct {
	modelProvider modelprovider.ModelProvider
	expert        expert.Expert // used by orchestrator for multi-step plans
	summarizer    agentutils.Summarizer
	toolRegistry  map[string]tool.Tool
	// toolWrapSvc wraps sub-agent tools with HITL approval, audit logging,
	// and caching. When nil, sub-agent tools execute without HITL gating.
	toolWrapSvc   *toolwrap.Service
	workingMemory *memory.WorkingMemory
	episodic      memory.EpisodicMemory
}

// NewCreateAgentTool creates a tool that spawns sub-agents with dynamic tool
// subsets. The llmModel is the LLM to use for sub-agents. The toolRegistry is
// a name→tool map of all available tools the sub-agent can choose from.
// The optional toolWrapSvc, when provided, wraps sub-agent tools with HITL
// approval gating, audit logging, and file-read caching — ensuring sub-agents
// cannot execute write tools without human approval.
func NewCreateAgentTool(
	modelProvider modelprovider.ModelProvider,
	expert expert.Expert,
	summarizer agentutils.Summarizer,
	toolRegistry ToolRegistry,
	workingMemory *memory.WorkingMemory,
	episodic memory.EpisodicMemory,
	toolWrapSvc *toolwrap.Service,
) tool.Tool {
	// Build description listing available tools
	var toolList []string
	for name := range toolRegistry {
		toolList = append(toolList, name)
	}

	t := &createAgentTool{
		modelProvider: modelProvider,
		expert:        expert,
		summarizer:    summarizer,
		toolRegistry:  toolRegistry,
		toolWrapSvc:   toolWrapSvc,
		workingMemory: workingMemory,
		episodic:      episodic,
	}

	desc := fmt.Sprintf(
		"Spawn a sub-agent with selected tools for multi-step tasks. "+
			"task_type: tool_calling (file/shell, fastest), planning (reasoning), "+
			"terminal_calling (CLI), novel_reasoning (creative). "+
			"Give only needed tools. Batch related work into one agent; "+
			"spawn parallel agents for independent tasks.\n\n"+
			"MULTI-STEP PLANS: For complex tasks, provide 'steps' with subgoals "+
			"and 'flow_type' (sequence/parallel/fallback). Each step becomes an "+
			"independent agent node coordinated by the chosen flow. Use parallel "+
			"for independent tasks, sequence for dependent steps.\n\n"+
			"Set max_tool_iterations and max_llm_calls based on task complexity: "+
			"simple lookups: 3-5, file edits: 10-15, multi-step: 15-25. "+
			"Avoid excessive values — they waste time and money.\n\n"+
			"Available tools: %s",
		strings.Join(toolList, ", "),
	)

	return function.NewFunctionTool(
		t.execute,
		function.WithName("create_agent"),
		function.WithDescription(desc),
	)
}

func (t *createAgentTool) execute(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.execute", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)
	logr.Info("create_agent invoked", "tool_names", req.ToolNames, "task_type", req.TaskType, "steps", len(req.Steps))

	// Multi-step plan: delegate to orchestrator (paper's Expand action).
	if len(req.Steps) > 0 {
		return t.executePlan(ctx, req)
	}

	// Select tools from registry, excluding create_agent to prevent
	// recursive sub-agent spawning (parent → sub → sub → ...).
	var selectedTools []tool.Tool
	if len(req.ToolNames) == 0 {
		for name, tl := range t.toolRegistry {
			if name == "create_agent" {
				continue
			}
			selectedTools = append(selectedTools, tl)
		}
	} else {
		for _, name := range req.ToolNames {
			if name == "create_agent" {
				continue
			}
			if tl, ok := t.toolRegistry[name]; ok {
				selectedTools = append(selectedTools, tl)
			}
		}
	}

	// Always inject ask_clarifying_question so sub-agents can ask the user
	// for clarification when they encounter ambiguity mid-task.
	const clarifyToolName = "ask_clarifying_question"
	if len(req.ToolNames) > 0 { // only inject when specific tools were requested
		hasClarify := false
		for _, name := range req.ToolNames {
			if name == clarifyToolName {
				hasClarify = true
				break
			}
		}
		if !hasClarify {
			if tl, ok := t.toolRegistry[clarifyToolName]; ok {
				selectedTools = append(selectedTools, tl)
			}
		}
	}

	// FRAMEWORK INVARIANT: Sub-agents NEVER get send_message.
	// The orchestrator (parent agent) is the ONLY entity that communicates
	// with users. Sub-agents return results via their output, which the
	// parent can then format and send. This prevents the N-copies bug
	// where each LLM iteration inside a sub-agent fires send_message.
	for i := len(selectedTools) - 1; i >= 0; i-- {
		if selectedTools[i].Declaration().Name == "send_message" {
			logr.Info("stripping send_message from sub-agent tools (framework invariant)")
			selectedTools = append(selectedTools[:i], selectedTools[i+1:]...)
		}
	}

	logr.Info("sub-agent tools selected", "count", len(selectedTools))

	// Wrap sub-agent tools with HITL approval, audit logging, and caching.
	// This ensures every sub-agent tool call (run_shell, save_file, etc.)
	// goes through the same approval gate as parent-agent tools.
	// Extract per-request fields (EventChan, ThreadID, RunID) from context
	// so HITL approval events propagate to the UI correctly.
	threadID := agui.ThreadIDFromContext(ctx)
	runID := agui.RunIDFromContext(ctx)
	evChan := agui.EventChanFromContext(ctx)
	logr.Info("wrapping sub-agent tools with HITL",
		"threadID", threadID,
		"runID", runID,
		"hasEventChan", evChan != nil,
	)
	selectedTools = t.toolWrapSvc.Wrap(selectedTools, toolwrap.WrapRequest{
		EventChan:     evChan,
		ThreadID:      threadID,
		RunID:         runID,
		MessageOrigin: messenger.MessageOriginFrom(ctx),
	})

	if req.TaskType == "" {
		req.TaskType = modelprovider.TaskPlanning
	}

	modelToUse, err := t.modelProvider.GetModel(ctx, req.TaskType)
	if err != nil {
		return CreateAgentResponse{
			Status: "error",
			Output: fmt.Sprintf("failed to get model: %v", err),
		}, nil
	}
	// Apply reasonable defaults and hard caps.
	// Minimums ensure simple tasks finish; maximums prevent runaway loops.
	if req.MaxToolIterations < 3 {
		req.MaxToolIterations = 3
	}
	const maxToolIterCap = 10
	if req.MaxToolIterations > maxToolIterCap {
		logr.Warn("capping max_tool_iterations", "requested", req.MaxToolIterations, "cap", maxToolIterCap)
		req.MaxToolIterations = maxToolIterCap
	}
	if req.MaxLLMCalls < 8 {
		req.MaxLLMCalls = 8
	}
	const maxLLMCallsCap = 15
	if req.MaxLLMCalls > maxLLMCallsCap {
		logr.Warn("capping max_llm_calls", "requested", req.MaxLLMCalls, "cap", maxLLMCallsCap)
		req.MaxLLMCalls = maxLLMCallsCap
	}

	// Build a set of available tool names for dynamic instruction generation.
	availableToolNames := make(map[string]bool, len(selectedTools))
	var toolNameList []string
	for _, tl := range selectedTools {
		name := tl.Declaration().Name
		availableToolNames[name] = true
		toolNameList = append(toolNameList, name)
	}
	logr.Info("sub-agent available tools", "tools", toolNameList)

	// Base instruction — dynamically built based on available tools.
	instruction := "You are a focused sub-agent. Complete the given task using ONLY your available tools. " +
		"Be concise — return the essential result without commentary. " +
		"If working memory already contains relevant data, use it instead of re-reading files. " +
		"IMPORTANT: File operation tools only accept RELATIVE paths under the workspace directory. " +
		"OUTPUT: Return your result as text in your final response. Do NOT try to call send_message — " +
		"you do not have it. The parent agent will handle all user communication. "

	instruction += "Do not rewrite the same file multiple times unless fixing an error. Write files once and move to the next task. " +
		"ERROR BUDGET: If the same tool (e.g. web_search) fails 2 times — even with DIFFERENT arguments — " +
		"stop calling that tool. Report the failure to the user instead of retrying with rephrased queries. " +
		"ANTI-LOOP: After calling a tool, process its result immediately. " +
		"NEVER call the same tool with the same arguments more than once — if you already received a result, use it directly. " +
		"NEVER re-search with slightly different wording — if a search returned results, extract the answer from what you have. " +
		"If a search FAILED due to errors or rate limits, do NOT retry with different wording. Report the failure. " +
		"Once you have the data you need, summarize it and return your final answer. Do NOT repeat the answer more than once. " +
		"DO NOT ASSUME: If the goal is ambiguous, critical details are missing (e.g. which environment, branch, or target), " +
		"or multiple valid approaches exist, use ask_clarifying_question to ask the user before proceeding. " +
		"Never guess or fill in blanks — ask first, act second. " +
		"CRITICAL: You may ONLY call tools that are in your available tool set. Do NOT attempt to call tools that are not listed. " +
		"JUSTIFICATION: When calling any tool, include a \"_justification\" field in the arguments explaining why this action is necessary."

	// Inject shared working memory context so sub-agent knows what has been done
	if t.workingMemory != nil {
		snapshot := t.workingMemory.Snapshot()
		if len(snapshot) > 0 {
			instruction += "\n\nSHARED WORKING MEMORY (Read-Only):\n"
			for k, v := range snapshot {
				// Truncate values if they are too long to fit in context efficiently
				val := v
				if len(val) > 200 {
					val = val[:197] + "..."
				}
				instruction += fmt.Sprintf("- %s: %s\n", k, val)
			}
		}
	}

	// Create a fresh sub-agent with only the selected tools
	subAgent := llmagent.New(
		req.AgentName,
		llmagent.WithModel(modelToUse),
		llmagent.WithTools(selectedTools),
		llmagent.WithInstruction(instruction),
		llmagent.WithDescription("Focused sub-agent for delegated tasks"),
		llmagent.WithEnableParallelTools(true),
		llmagent.WithMaxLLMCalls(req.MaxLLMCalls),
		llmagent.WithAddCurrentTime(true),
		llmagent.WithTimeFormat(time.RFC3339),
		llmagent.WithMaxToolIterations(req.MaxToolIterations),
		// Token optimization: Only include current request context, not full
		// history, preventing unbounded context growth (50-70% savings).
		llmagent.WithMessageFilterMode(llmagent.RequestContext),
	)

	// Run via a one-shot runner with isolated session.
	// Wire session summarizer so framework handles context compression
	// instead of genie's manual accumulateContext() truncation.
	sessionSvc := inmemory.NewSessionService(
		inmemory.WithSummarizer(summary.NewSummarizer(
			modelToUse,
			summary.WithTokenThreshold(2000),
			summary.WithName("subagent-summarizer"),
		)),
	)
	r := runner.NewRunner(
		req.AgentName,
		subAgent,
		runner.WithSessionService(sessionSvc),
	)
	defer func(startTime time.Time) {
		_ = r.Close()
		logr.Info("sub-agent execution completed", "duration", time.Since(startTime).String())
	}(time.Now())

	// Explicitly start a span for the sub-agent execution to ensure proper
	// nesting in traces. The tool invoker might not have propagated the
	// tool span context correctly to the runner otherwise.
	tracer := otel.Tracer("genie")
	runCtx, span := tracer.Start(ctx, req.AgentName+" execution")
	defer span.End()

	// Wall-clock deadline prevents sub-agents from running indefinitely
	// (e.g. browser tools stuck in 60s timeout loops).
	const subAgentTimeout = 2 * time.Minute
	runCtx, cancel := context.WithTimeout(runCtx, subAgentTimeout)
	defer cancel()

	// Retry transient upstream LLM errors (503 / rate-limit / overloaded)
	// so that temporary provider capacity issues don't fail the sub-agent.
	var evCh <-chan *event.Event
	runErr := retrier.Retry(runCtx, func() error {
		var err error
		evCh, err = r.Run(runCtx, "parent", req.AgentName, model.NewUserMessage(req.Goal))
		return err
	},
		retrier.WithAttempts(3),
		retrier.WithBackoffDuration(5*time.Second),
		retrier.WithRetryIf(expert.IsTransientError),
		retrier.WithOnRetry(func(attempt int, err error) {
			logr.Warn("transient LLM error in sub-agent, retrying",
				"attempt", attempt, "error", err.Error())
		}),
	)
	if runErr != nil {
		return CreateAgentResponse{
			Status: "error",
			Output: fmt.Sprintf("sub-agent failed to start: %v", runErr),
		}, nil
	}

	// Collect response
	var sb strings.Builder
	for ev := range evCh {
		if ev.Error != nil {
			return CreateAgentResponse{
				Status: "error",
				Output: fmt.Sprintf("sub-agent error: %s", ev.Error.Message),
			}, nil
		}
		if ev.Response != nil {
			for _, choice := range ev.Choices {
				if choice.Message.Role == model.RoleAssistant && choice.Message.Content != "" {
					sb.WriteString(choice.Message.Content)
				}
			}
		}
	}

	result := sb.String()
	logr.Info("sub-agent execution completed", "output_length", len(result))

	// Store sub-agent result in working memory so the parent can retrieve
	// it via memory_search and compose a single user-facing message.
	if t.workingMemory != nil && result != "" {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		t.workingMemory.Store(wmKey, result)
		logr.Info("sub-agent result stored in working memory", "key", wmKey, "length", len(result))
	}

	// Store successful result as an episode for future in-context retrieval.
	// Paper Section 4.2: episodic memory stores subgoal-level experiences
	// so future agent nodes with similar goals get relevant examples.
	if t.episodic != nil && result != "" {
		trajectory := result
		const maxTrajectoryRunes = 500
		runes := []rune(trajectory)
		if len(runes) > maxTrajectoryRunes {
			trajectory = string(runes[:maxTrajectoryRunes]) + "... (truncated)"
		}
		t.episodic.Store(ctx, memory.Episode{
			Goal:       req.Goal,
			Trajectory: trajectory,
			Status:     memory.EpisodeSuccess,
		})
		logr.Info("sub-agent result stored in episodic memory", "goal", toolwrap.TruncateForAudit(req.Goal, 60))
	}

	// Summarize large sub-agent output to keep context concise for the parent agent.
	if t.summarizer != nil && len(result) > summarizeThreshold {
		logr.Info("summarizing large sub-agent output", "original_length", len(result), "threshold", summarizeThreshold)
		summarized, err := t.summarizer.Summarize(ctx, agentutils.SummarizeRequest{
			Content:              result,
			RequiredOutputFormat: agentutils.OutputFormatMarkdown,
		})
		if err == nil {
			logr.Info("sub-agent output summarized", "original_length", len(result), "summarized_length", len(summarized))
			result = summarized
		} else {
			logr.Warn("sub-agent output summarization failed, using original", "error", err)
		}
	}

	return CreateAgentResponse{
		// Prefix tells the parent agent not to re-present this data, since
		// the sub-agent already streamed it to the user during execution.
		Output: "[SHOWN TO USER] Do not repeat; confirm completion or add NEW info only.\n" + result,
		Status: "success",
	}, nil
}

func (req CreateAgentRequest) flow(ctx context.Context) ControlFlowType {
	// Map flow_type string to ControlFlowType.
	switch req.Flow {
	case "parallel":
		return ControlFlowParallel
	case "fallback":
		return ControlFlowFallback
	case "sequence", "":
		return ControlFlowSequence
	default:
		logger.GetLogger(ctx).Warn("unknown flow_type, defaulting to sequence", "flow_type", req.Flow)
	}
	return ControlFlowSequence
}

// executePlan handles multi-step plan requests by delegating to ExecutePlan.
// This implements the paper's "Expand" action: the parent agent decomposes
// a goal into subgoals with a control flow type, and the orchestrator builds
// a graph from those subgoals.
func (t *createAgentTool) executePlan(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.executePlan", "name", req.AgentName, "steps", len(req.Steps))

	// Map flow_type string to ControlFlowType.
	flow := req.flow(ctx)

	plan := Plan{
		Flow:  flow,
		Steps: req.Steps,
	}

	logr.Info("executing multi-step plan", "flow", string(flow))

	maxDecisions := req.MaxLLMCalls
	if maxDecisions < 8 {
		maxDecisions = 8
	}
	const maxLLMCallsCap = 15
	if maxDecisions > maxLLMCallsCap {
		maxDecisions = maxLLMCallsCap
	}

	// Get the event channel from context for progress events.
	evChan := agui.EventChanFromContext(ctx)

	result, err := ExecutePlan(ctx, plan, OrchestratorConfig{
		Expert:        t.expert,
		WorkingMemory: t.workingMemory,
		Episodic:      t.episodic,
		MaxDecisions:  maxDecisions,
		EventChan:     evChan,
		ToolRegistry:  t.toolRegistry,
		SenderContext: "",
		ToolWrapSvc:   t.toolWrapSvc,
		WrapRequest: toolwrap.WrapRequest{
			EventChan:     evChan,
			ThreadID:      agui.ThreadIDFromContext(ctx),
			RunID:         agui.RunIDFromContext(ctx),
			MessageOrigin: messenger.MessageOriginFrom(ctx),
		},
	})
	if err != nil {
		return CreateAgentResponse{
			Status: "error",
			Output: fmt.Sprintf("plan execution failed: %v", err),
		}, nil
	}

	// Combine step outputs into a single response.
	var sb strings.Builder
	for _, step := range plan.Steps {
		if out, ok := result.Outputs[step.Name]; ok {
			fmt.Fprintf(&sb, "## %s\n\n%s\n\n", step.Name, out)
		}
	}
	output := sb.String()

	// Store combined result in working memory.
	if t.workingMemory != nil && output != "" {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		t.workingMemory.Store(wmKey, output)
		logr.Info("plan result stored in working memory", "key", wmKey, "length", len(output))
	}

	status := "success"
	if result.Status != Success {
		status = "partial"
	}

	logr.Info("plan execution completed", "status", status, "output_length", len(output))

	return CreateAgentResponse{
		Output: output,
		Status: status,
	}, nil
}
