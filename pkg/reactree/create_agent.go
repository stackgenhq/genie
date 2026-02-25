package reactree

import (
	"context"
	"fmt"
	"strings"

	"time"

	"github.com/stackgenhq/genie/pkg/agentutils"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/dedup"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/retrier"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/toolwrap"
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

const (
	summarizeThreshold  = 2000
	CreateAgentToolName = "create_agent"
	defaultTimeout      = 5 * time.Minute
	minTimeout          = 30 * time.Second
	maxTimeout          = 10 * time.Minute

	minToolIterCap = 5
	maxToolIterCap = 50

	minLLMCallsCap = 8
	maxLLMCallsCap = 60
)

// CreateAgentRequest is the input for the create_agent tool.
type CreateAgentRequest struct {
	AgentName         string                 `json:"agent_name" jsonschema:"description=Name of the sub-agent,required"`
	Goal              string                 `json:"goal" jsonschema:"description=The goal or task for the sub-agent to accomplish,required"`
	ToolNames         []string               `json:"tool_names,omitempty" jsonschema:"description=Names of tools to give the sub-agent. If empty all tools are provided."`
	TaskType          modelprovider.TaskType `json:"task_type,omitempty" jsonschema:"description=Selects the model best suited for the sub-agent. planning: complex reasoning and multi-step analysis and code changes (default — use for most tasks). tool_calling: straightforward function calls and data extraction. terminal_calling: shell commands and CLI workflows. efficiency: quick read-only lookups and simple searches."`
	MaxToolIterations int                    `json:"max_tool_iterations,omitempty" jsonschema:"description=Maximum tool iterations. Scale to complexity: simple lookups 5-10 and file edits 15-25 and multi-step/infrastructure 30-50,required"`
	MaxLLMCalls       int                    `json:"max_llm_calls,omitempty" jsonschema:"description=Maximum LLM calls. Scale to complexity: simple lookups 5-10 and file edits 15-25 and multi-step/infrastructure 30-60,required"`
	TimeoutSeconds    float64                `json:"timeout_seconds,omitempty" jsonschema:"description=Hard timeout in seconds for the sub-agent. Scale to complexity: simple lookups 60-120 and multi-step 180-300. Default 300 (5 min). Prevents hung agents."`

	// SummarizeOutput controls whether large sub-agent output is summarized
	// before returning to the parent agent. When false (default), the raw
	// output is returned as-is, preserving all detail. Set to true only when
	// the output is expected to be very large and a condensed version suffices.
	SummarizeOutput bool `json:"summarize_output,omitempty" jsonschema:"description=When true the sub-agent output is summarized if it exceeds 2000 chars. Default false — raw output is returned preserving all detail. Only enable when a condensed summary is acceptable."`

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

func (req CreateAgentRequest) timeoutSeconds() float64 {
	// Clamp timeout: floor prevents overly tight deadlines, ceiling
	// prevents runaway agents. Default 5 min if not specified.
	if req.TimeoutSeconds <= 0 {
		return defaultTimeout.Seconds()
	}
	return min(max(req.TimeoutSeconds, minTimeout.Seconds()), maxTimeout.Seconds())
}

// clampedMaxToolIterations returns MaxToolIterations clamped to
// [minToolIterCap, maxToolIterCap]. Ensures sub-agents always get
// a workable iteration budget without unbounded loops.
func (req CreateAgentRequest) clampedMaxToolIterations() int {
	return min(max(req.MaxToolIterations, minToolIterCap), maxToolIterCap)
}

// clampedMaxLLMCalls returns MaxLLMCalls clamped to
// [minLLMCallsCap, maxLLMCallsCap]. Prevents sub-agents from
// making too few calls to be useful or too many to be cost-effective.
func (req CreateAgentRequest) clampedMaxLLMCalls() int {
	return min(max(req.MaxLLMCalls, minLLMCallsCap), maxLLMCallsCap)
}

// resolveStatus determines the final status and output string for a
// sub-agent run based on whether it timed out, produced errors, or
// returned results. Without this, every caller of executeInner would
// duplicate the same branching logic for timeout/error annotation.
func (req CreateAgentRequest) resolveStatus(timedOut bool, lastErr, result string) (status, output string) {
	output = result
	status = "success"

	if timedOut {
		status = "partial"
		prefix := fmt.Sprintf("[TIME LIMIT REACHED] The sub-agent %q ran out of time. Here is what was found before the deadline:\n\n", req.AgentName)
		if output == "" {
			output = prefix + "No output was captured before the deadline. The sub-agent may have been waiting for external calls (web_search, http_request) to complete."
		} else {
			output = prefix + output
		}
		return status, output
	}

	if lastErr != "" && output == "" {
		status = "error"
		output = fmt.Sprintf("sub-agent error: %s", lastErr)
	}
	return status, output
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
	toolRegistry  *tools.Registry // full registry (used by main agent description)
	// subAgentRegistry is the tool registry with orchestration-only tools
	// (create_agent, send_message) stripped. Sub-agents must not recursively
	// spawn agents or send messages directly — only the main agent can.
	subAgentRegistry *tools.Registry
	// toolWrapSvc wraps sub-agent tools with HITL approval, audit logging,
	// and caching. When nil, sub-agent tools execute without HITL gating.
	toolWrapSvc   *toolwrap.Service
	workingMemory *memory.WorkingMemory
	episodic      memory.EpisodicMemory
	description   string

	// inflight deduplicates identical parallel create_agent calls
	// from the LLM (same agent_name + goal). Backed by singleflight.
	inflight dedup.Group[CreateAgentResponse]
}

// orchestrationOnlyTools lists tool names that are available to the main agent
// but must NOT be given to sub-agents. Sub-agents must not recursively spawn
// agents or send messages directly — those are orchestration concerns.
var orchestrationOnlyTools = []string{"create_agent", messenger.ToolName}

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
	toolRegistry *tools.Registry,
	workingMemory *memory.WorkingMemory,
	episodic memory.EpisodicMemory,
	toolWrapSvc *toolwrap.Service,
) *createAgentTool {
	// Build a sub-agent registry that excludes orchestration-only tools.
	// Sub-agents must not call create_agent (no recursive spawning) or
	// send_message (only the main agent orchestrates user communication).
	subAgentRegistry := toolRegistry.Exclude(orchestrationOnlyTools...)

	// Build description listing tools available to sub-agents.
	toolList := subAgentRegistry.ToolNames()

	t := &createAgentTool{
		modelProvider:    modelProvider,
		expert:           expert,
		summarizer:       summarizer,
		toolRegistry:     toolRegistry,
		subAgentRegistry: subAgentRegistry,
		toolWrapSvc:      toolWrapSvc,
		workingMemory:    workingMemory,
		episodic:         episodic,
	}

	t.description = fmt.Sprintf(
		"Spawn a sub-agent with selected tools for multi-step tasks. "+
			"task_type selects the model: planning (complex reasoning, code changes — default, best for most tasks), "+
			"tool_calling (simple API calls, data extraction), "+
			"terminal_calling (shell/CLI work), efficiency (quick read-only lookups). "+
			"When in doubt, use planning. "+
			"Give only needed tools. Batch related work into one agent; "+
			"spawn parallel agents for independent tasks.\n\n"+
			"MULTI-STEP PLANS: For complex tasks, provide 'steps' with subgoals "+
			"and 'flow_type' (sequence/parallel/fallback). Each step becomes an "+
			"independent agent node coordinated by the chosen flow. Use parallel "+
			"for independent tasks, sequence for dependent steps.\n\n"+
			"Set max_tool_iterations and max_llm_calls based on task complexity: "+
			"simple lookups: 5-10, file edits: 15-25, multi-step/infrastructure: 30-50. "+
			"Use higher values for infrastructure tasks that involve discovery and retries. "+
			"Set timeout_seconds to limit wall-clock time: simple 60-120, multi-step 180-300.\n\n"+
			"Available tools: %s",
		strings.Join(toolList, ", "),
	)

	return t
}

func (t *createAgentTool) GetTool() tool.Tool {
	return function.NewFunctionTool(
		t.execute,
		function.WithName("create_agent"),
		function.WithDescription(t.description),
	)
}

func (t *createAgentTool) execute(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.execute", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)
	logr.Info("create_agent invoked", "tool_names", req.ToolNames, "task_type", req.TaskType, "steps", len(req.Steps))

	// Dedup: identical parallel calls (same name+goal) are collapsed
	// via singleflight so only one sub-agent runs.
	dedupKey := req.AgentName + ":" + req.Goal
	resp, err, shared := t.inflight.Do(dedupKey, func() (CreateAgentResponse, error) {
		return t.executeInner(ctx, req)
	})
	if shared {
		logr.Warn("duplicate create_agent call coalesced", "agent_name", req.AgentName)
	}
	return resp, err
}

func (t *createAgentTool) executeInner(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.executeInner", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)

	// Multi-step plan: delegate to orchestrator (paper's Expand action).
	if len(req.Steps) > 0 {
		return t.executePlan(ctx, req)
	}

	// Wrap sub-agent tools with HITL approval, audit logging, and caching.
	// This ensures every sub-agent tool call (run_shell, save_file, etc.)
	// goes through the same approval gate as parent-agent tools.
	// Extract per-request fields (ThreadID, RunID) from context
	// so HITL approval events propagate to the UI correctly.
	threadID := agui.ThreadIDFromContext(ctx)
	runID := agui.RunIDFromContext(ctx)
	logr.Info("wrapping sub-agent tools with HITL",
		"threadID", threadID,
		"runID", runID,
	)
	// Scope sub-agent tools to only the ones the planner requested.
	// If req.ToolNames is empty, all sub-agent tools are available.
	scopedRegistry := t.subAgentRegistry
	if len(req.ToolNames) > 0 {
		scopedRegistry = scopedRegistry.Include(req.ToolNames...)
	}
	selectedTools := t.toolWrapSvc.Wrap(scopedRegistry.AllTools(), toolwrap.WrapRequest{})

	// Working memory is injected into the prompt automatically.
	// No scratchpad tools needed — follows trpc-agent-go pattern.

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
	req.MaxToolIterations = req.clampedMaxToolIterations()
	req.MaxLLMCalls = req.clampedMaxLLMCalls()

	// Build a list of tool names for the sub-agent instruction and logging.
	var toolNameList []string
	for _, tl := range selectedTools {
		toolNameList = append(toolNameList, tl.Declaration().Name)
	}
	logr.Info("sub-agent available tools", "tools", toolNameList)

	// Build sub-agent instruction from the shared builder.
	// This ensures plan-step agents and single sub-agents get the
	// same focused instruction (no persona contamination).
	instruction := buildSubAgentInstruction(toolNameList)

	// Create a fresh sub-agent with only the selected tools
	subAgent := llmagent.New(
		req.AgentName,
		llmagent.WithModels(modelToUse),
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
			modelToUse.GetAny(),
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

	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, time.Duration(req.timeoutSeconds())*time.Second)
	defer cancelTimeout()
	logr.Info("sub-agent timeout set", "timeout_seconds", req.timeoutSeconds())

	tracer := otel.Tracer("genie")
	runCtx, span := tracer.Start(timeoutCtx, req.AgentName+" execution")
	defer span.End()

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

	// Collect response — accumulate partial output even if errors occur.
	// On context deadline, we keep whatever was gathered instead of losing it.
	var sb strings.Builder
	var lastErr string
	timedOut := false
	for ev := range evCh {
		if ev.Error != nil {
			lastErr = ev.Error.Message
			// Don't early-return: keep collecting any remaining events.
			// The channel will close shortly and we'll handle the error below.
			logr.Warn("sub-agent event error (continuing to collect output)", "error", lastErr)
			continue
		}
		if ev.Response != nil {
			for _, choice := range ev.Choices {
				if choice.Message.Role == model.RoleAssistant && choice.Message.Content != "" {
					sb.WriteString(choice.Message.Content)
				}
			}
		}
	}

	// Check if the context deadline caused the sub-agent to stop.
	if runCtx.Err() != nil {
		timedOut = true
		logr.Warn("sub-agent context expired, returning partial results",
			"error", runCtx.Err().Error(),
			"partial_output_length", sb.Len())
	}

	result := sb.String()

	// When the sub-agent produced no LLM output (e.g. deadline hit mid-generation),
	// check working memory for data stored by tool calls that completed before
	// the deadline (http_request → summarize → memory_store pipeline).
	if result == "" && t.workingMemory != nil {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		if stored, ok := t.workingMemory.Recall(wmKey); ok && stored != "" {
			result = stored
			logr.Info("recovered sub-agent output from working memory", "key", wmKey, "length", len(result))
		}
	}

	logr.Info("sub-agent execution completed",
		"output_length", len(result), "timed_out", timedOut, "had_error", lastErr != "")

	status, result := req.resolveStatus(timedOut, lastErr, result)

	// Store sub-agent result in working memory so the parent can retrieve
	// it via memory_search and compose a single user-facing message.
	if t.workingMemory != nil && result != "" {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		t.workingMemory.Store(wmKey, result)
		logr.Info("sub-agent result stored in working memory", "key", wmKey, "length", len(result))
	}

	// Store result as an episode for future in-context retrieval.
	// Paper Section 4.2: episodic memory stores subgoal-level experiences
	// so future agent nodes with similar goals get relevant examples.
	if t.episodic != nil && result != "" {
		trajectory := result
		const maxTrajectoryRunes = 500
		runes := []rune(trajectory)
		if len(runes) > maxTrajectoryRunes {
			trajectory = string(runes[:maxTrajectoryRunes]) + "... (truncated)"
		}
		episodeStatus := memory.EpisodeSuccess
		if timedOut {
			episodeStatus = memory.EpisodeFailure
		}
		t.episodic.Store(ctx, memory.Episode{
			Goal:       req.Goal,
			Trajectory: trajectory,
			Status:     episodeStatus,
		})
		logr.Info("sub-agent result stored in episodic memory", "goal", toolwrap.TruncateForAudit(req.Goal, 60))
	}

	// Summarize large sub-agent output only when the caller explicitly opted in.
	// By default raw output is returned so the parent agent sees full detail.
	// Skip summarization if we already timed out — the summarizer would also fail.
	if req.SummarizeOutput && !timedOut && t.summarizer != nil && len(result) > summarizeThreshold {
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
		Output: result,
		Status: status,
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

	req.MaxLLMCalls = req.clampedMaxLLMCalls()

	result, err := ExecutePlan(ctx, plan, OrchestratorConfig{
		Expert:        t.expert,
		WorkingMemory: t.workingMemory,
		Episodic:      t.episodic,
		MaxDecisions:  req.MaxLLMCalls,
		ToolRegistry:  t.subAgentRegistry, // use filtered registry — no create_agent/send_message
		ToolWrapSvc:   t.toolWrapSvc,
		WrapRequest:   toolwrap.WrapRequest{},
		ModelProvider: t.modelProvider,
	})
	if err != nil {
		return CreateAgentResponse{
			Status: "error",
			Output: fmt.Sprintf("plan execution failed: %v", err),
		}, nil
	}

	// Combine step outputs into a single response.
	var sb strings.Builder
	var succeeded, failed []string
	for _, step := range plan.Steps {
		if out, ok := result.Outputs[step.Name]; ok && out != "" {
			fmt.Fprintf(&sb, "## %s\n\n%s\n\n", step.Name, out)
			succeeded = append(succeeded, step.Name)
		} else {
			failed = append(failed, step.Name)
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

	// When output is empty, provide rich context so the parent agent can
	// adapt its retry strategy instead of re-running the same failing plan.
	if output == "" && status != "success" {
		var detail strings.Builder
		detail.WriteString("⚠️ Research swarm completed with no results.\n\n")
		if len(failed) > 0 {
			detail.WriteString("**Failed steps:** " + strings.Join(failed, ", ") + "\n\n")
		}
		detail.WriteString("**Suggested next steps:**\n")
		detail.WriteString("- If web_search failed, use `http_request` to visit known websites directly (e.g. the company homepage, Wikipedia, Crunchbase)\n")
		detail.WriteString("- If http_request timed out, try fewer URLs per agent\n")
		detail.WriteString("- Split the work differently or use alternative data sources\n")
		output = detail.String()
	} else if len(failed) > 0 && len(succeeded) > 0 {
		// Append a note about which steps failed so the parent agent knows.
		fmt.Fprintf(&sb, "\n---\n⚠️ Partial results: the following steps did not produce output: %s. Consider re-running them with alternative tools.\n", strings.Join(failed, ", "))
		output = sb.String()
	}

	// Prefix with status so the LLM sees it naturally in prose.
	if status == "partial" && !strings.HasPrefix(output, "⚠️") {
		output = "⚠️ [Partial results] " + output
	}

	logr.Info("plan execution completed", "status", status, "output_length", len(output),
		"succeeded", succeeded, "failed", failed)

	return CreateAgentResponse{
		Output: output,
		Status: status,
	}, nil
}
