// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stackgenhq/genie/pkg/agentutils"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/dedup"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/halguard"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/retrier"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	Context           string                 `json:"context,omitempty" jsonschema:"description=Optional historical or contextual data to provide to the sub-agent alongside the goal."`
	ToolNames         []string               `json:"tool_names,omitempty" jsonschema:"description=Names of tools to give the sub-agent. If empty all tools are provided."`
	TaskType          modelprovider.TaskType `json:"task_type,omitempty" jsonschema:"description=Selects the model best suited for the sub-agent. planning: complex reasoning and multi-step analysis and code changes (default — use for most tasks). coding: pure code generation and algorithmic problem solving and script writing. terminal_calling: shell commands and CLI workflows. efficiency: quick read-only lookups and simple searches."`
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

func (req CreateAgentRequest) timeout() time.Duration {
	// Clamp timeout: floor prevents overly tight deadlines, ceiling
	// prevents runaway agents. Default 5 min if not specified.
	if req.TimeoutSeconds <= 0 {
		return defaultTimeout
	}
	seconds := min(max(req.TimeoutSeconds, minTimeout.Seconds()), maxTimeout.Seconds())
	return time.Duration(seconds) * time.Second
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
// returned results. The partialToolResults parameter carries content
// captured from tool result events during the run — when the sub-agent
// errors without producing a final response, these partial findings are
// included so the parent agent can still use what was learned.
// Without this, every caller of executeInner would duplicate the same
// branching logic for timeout/error annotation.
func (req CreateAgentRequest) resolveStatus(timedOut bool, lastErr, result, partialToolResults string) (status AgentStatus, output string) {
	output = result
	status = AgentStatusSuccess

	if timedOut {
		status = AgentStatusPartial
		prefix := fmt.Sprintf("[TIME LIMIT REACHED] The sub-agent %q ran out of time. Here is what was found before the deadline:\n\n", req.AgentName)
		if output == "" && partialToolResults != "" {
			output = prefix + partialToolResults
		} else if output == "" {
			output = prefix + "No output was captured before the deadline. The sub-agent may have been waiting for external calls (web_search, http_request) to complete."
		} else {
			output = prefix + output
		}
		return status, output
	}

	if lastErr != "" && output == "" {
		if partialToolResults != "" {
			status = AgentStatusPartial
			output = fmt.Sprintf("[BUDGET EXCEEDED] The sub-agent %q ran out of LLM calls. "+
				"Here is what it learned before the limit:\n\n%s\n\nError: %s",
				req.AgentName, partialToolResults, lastErr)
		} else {
			status = AgentStatusError
			output = fmt.Sprintf("sub-agent error: %s", lastErr)
		}
	}
	return status, output
}

// AgentStatus represents the status of a sub-agent execution.
type AgentStatus int

const (
	AgentStatusSuccess AgentStatus = iota
	AgentStatusPartial
	AgentStatusError
	AgentStatusToolUseFailure
	AgentStatusVerifiedCorrected
)

func (s AgentStatus) String() string {
	switch s {
	case AgentStatusSuccess:
		return "success"
	case AgentStatusPartial:
		return "partial"
	case AgentStatusError:
		return "error"
	case AgentStatusToolUseFailure:
		return "tool_use_failure"
	case AgentStatusVerifiedCorrected:
		return "verified_corrected"
	default:
		return "unknown"
	}
}

func (s AgentStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *AgentStatus) UnmarshalJSON(b []byte) error {
	str := strings.Trim(string(b), `"`)
	switch str {
	case "success":
		*s = AgentStatusSuccess
	case "partial":
		*s = AgentStatusPartial
	case "error":
		*s = AgentStatusError
	case "tool_use_failure":
		*s = AgentStatusToolUseFailure
	case "verified_corrected":
		*s = AgentStatusVerifiedCorrected
	default:
		return fmt.Errorf("unknown status: %s", str)
	}
	return nil
}

// CreateAgentResponse is the output for the create_agent tool.
type CreateAgentResponse struct {
	Output string      `json:"output"`
	Status AgentStatus `json:"status"`
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
	// vectorStore is the vector memory store. Used to probe for emptiness
	// so retrieval-only sub-agents (memory_search, graph_*) can be skipped
	// when the store has no documents, avoiding futile LLM calls.
	vectorStore vector.IStore

	// toolIndex provides semantic search over tool declarations.
	// Used to resolve tools by goal description instead of listing
	// all tools in the description. Sub-agents do NOT get access to
	// this — they receive concrete tools via Registry.Include().
	toolIndex tools.SmartToolProvider

	description string

	// halGuard provides optional pre-delegation grounding checks and
	// post-execution output verification. When nil, sub-agents execute
	// without hallucination checks (backward compatible).
	halGuard          halguard.Guard
	halGuardThreshold float64 // PreCheckThreshold from config; 0 = use default (0.4)

	// inflight deduplicates identical parallel create_agent calls
	// from the LLM (same agent_name + goal). Backed by singleflight.
	inflight dedup.Group[CreateAgentResponse]

	skipSummarize bool

	// auditor is the optional durable audit trail for sub-agent creation.
	// When set, each sub-agent creation emits an audit event recording
	// the agent name, goal, delegated tools, and budget parameters.
	auditor audit.Auditor

	// failureReflector generates verbal reflections on sub-agent failures.
	// When set, failed episodes are stored with a detailed reflection
	// explaining what went wrong and what to try differently. When nil,
	// failures fall back to a generic reflection derived from the raw
	// trajectory and error output.
	failureReflector memory.FailureReflector

	// importanceScorer assigns 1-10 importance scores to episodes.
	// When set, the score is stored on the episode and influences
	// weighted retrieval. When nil, episodes use a neutral default.
	importanceScorer memory.ImportanceScorer

	// planAdvisor consults episodic memory and wisdom before executing
	// multi-step plans. When set, each step's context is enriched with
	// relevant past successes and failures.
	planAdvisor memory.PlanAdvisor
}

// CreateAgentOption configures the create_agent tool.
type CreateAgentOption func(*createAgentTool)

// WithSkipSummarizeMarker configures whether the tool should add a context
// marker telling the upstream summarizer to bypass summarization.
func WithSkipSummarizeMarker(skip bool) CreateAgentOption {
	return func(t *createAgentTool) {
		t.skipSummarize = skip
	}
}

// WithAuditor injects an auditor so sub-agent creation events are written
// to the durable audit trail.
func WithAuditor(a audit.Auditor) CreateAgentOption {
	return func(t *createAgentTool) { t.auditor = a }
}

// WithFailureReflector injects a failure reflector so sub-agent failures
// are stored with actionable verbal reflections instead of raw error output.
func WithFailureReflector(fr memory.FailureReflector) CreateAgentOption {
	return func(t *createAgentTool) { t.failureReflector = fr }
}

// WithImportanceScorer injects an importance scorer so sub-agent episodes
// receive 1-10 importance scores that influence weighted retrieval.
func WithImportanceScorer(is memory.ImportanceScorer) CreateAgentOption {
	return func(t *createAgentTool) { t.importanceScorer = is }
}

// WithPlanAdvisor injects a plan advisor so multi-step plans are enriched
// with relevant past experiences before execution.
func WithPlanAdvisor(pa memory.PlanAdvisor) CreateAgentOption {
	return func(t *createAgentTool) { t.planAdvisor = pa }
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
	subAgentRegistry *tools.Registry,
	workingMemory *memory.WorkingMemory,
	episodic memory.EpisodicMemory,
	toolWrapSvc *toolwrap.Service,
	vectorStore vector.IStore,
	halGuard halguard.Guard,
	toolIndex tools.SmartToolProvider,
	opts ...CreateAgentOption,
) *createAgentTool {
	t := &createAgentTool{
		modelProvider:    modelProvider,
		expert:           expert,
		summarizer:       summarizer,
		toolRegistry:     subAgentRegistry,
		subAgentRegistry: subAgentRegistry,
		toolWrapSvc:      toolWrapSvc,
		workingMemory:    workingMemory,
		episodic:         episodic,
		vectorStore:      vectorStore,
		halGuard:         halGuard,
		toolIndex:        toolIndex,
	}

	for _, opt := range opts {
		opt(t)
	}

	// Build description. If a tool index is available, instruct the LLM to
	// specify tools by capability instead of embedding a static list that
	// goes stale and causes hallucination.
	toolSection := "Use tool_names to specify which tools the sub-agent needs. " +
		"If unsure which tools exist, leave tool_names empty and all tools will be available."

	t.description = fmt.Sprintf(
		"Spawn a sub-agent with selected tools for multi-step tasks. "+
			"task_type selects the model: planning (complex reasoning, code changes — default, best for most tasks), "+
			"coding (pure code generation, algorithmic problem solving, script writing), "+
			"terminal_calling (shell/CLI work), efficiency (quick read-only lookups). "+
			"When in doubt, use planning. "+
			"Give only needed tools. Batch related work into one agent; "+
			"spawn parallel agents for independent tasks.\n\n"+
			"MULTI-STEP PLANS: For complex tasks, provide 'steps' with subgoals "+
			"and 'flow_type' (sequence/parallel/fallback). Each step becomes an "+
			"independent agent node coordinated by the chosen flow. Use parallel "+
			"for independent tasks, but AVOID sequence flow for strictly sequential "+
			"reasoning tasks as multi-agent handoffs degrade performance—instead, "+
			"use a single agent to handle all sequential steps. Batch related work into one "+
			"agent to avoid multi-agent coordination overhead, especially for tool-heavy tasks.\n\n"+
			"Set max_tool_iterations and max_llm_calls based on task complexity: "+
			"simple lookups: 5-10, file edits: 15-25, multi-step/infrastructure: 30-50. "+
			"Use higher values for infrastructure tasks that involve discovery and retries. "+
			"Set timeout_seconds to limit wall-clock time: simple 60-120, multi-step 180-300.\n\n"+
			"%s",
		toolSection,
	)

	return t
}

// SetHalGuardThreshold configures the pre-check confidence threshold.
// Values ≤ 0 are ignored (the default 0.4 is used instead).
func (t *createAgentTool) SetHalGuardThreshold(threshold float64) {
	if threshold > 0 {
		t.halGuardThreshold = threshold
	}
}

func (t *createAgentTool) GetTool() tool.Tool {
	return function.NewFunctionTool(
		t.execute,
		function.WithName("create_agent"),
		function.WithDescription(t.description),
	)
}

func (t *createAgentTool) execute(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	if t.skipSummarize {
		agentutils.SetSkipSummarize(ctx)
	}

	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.execute", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)
	logr.Info("create_agent invoked", "tool_names", req.ToolNames, "task_type", req.TaskType, "steps", len(req.Steps))

	// Audit: record sub-agent creation as a delegation-of-authority event.
	t.auditSubAgentCreation(ctx, req)

	// Dedup: identical parallel calls (same name+goal) are collapsed
	// via singleflight so only one sub-agent runs.
	dedupKey := req.AgentName + ":" + req.Goal
	resp, err, shared := t.inflight.Do(dedupKey, func() (CreateAgentResponse, error) {
		return t.executeInner(ctx, req)
	})
	if shared {
		logr.Warn("duplicate create_agent call coalesced", "agent_name", req.AgentName)
	}

	// Auto-retry on tool_use_failure: the sub-agent echoed commands as text
	// instead of calling run_shell. Retry once with a reinforced prompt that
	// leaves no ambiguity. Only retry once to avoid infinite loops.
	if err == nil && resp.Status == AgentStatusToolUseFailure {
		logr.Warn("auto-retrying sub-agent after tool_use_failure",
			"agent_name", req.AgentName, "attempt", 2)

		retryReq := req
		retryReq.Goal = "[RETRY — PREVIOUS ATTEMPT FAILED] " +
			"Your previous attempt FAILED because you echoed commands as text instead of executing them. " +
			"You MUST call the run_shell tool to execute the script below. " +
			"Do NOT output the script as text. Call run_shell with the script as the command argument.\n\n" +
			req.Goal
		retryReq.AgentName = req.AgentName + "-retry"

		resp, err = t.executeInner(ctx, retryReq)
		if resp.Status == AgentStatusToolUseFailure {
			logr.Error("sub-agent failed to use tools even after retry",
				"agent_name", req.AgentName)
		}
	}

	return resp, err
}

func (t *createAgentTool) verifyTools(ctx context.Context, req CreateAgentRequest) error {
	if len(req.ToolNames) == 0 {
		return nil
	}
	// Scope sub-agent tools to only the ones the planner requested.
	// If req.ToolNames is empty, all sub-agent tools are available.
	// Check for tools that were requested but are denied or unknown.
	// This catches the case where AGENTS.md/prompts instruct the LLM
	// to use tools that .genie.toml has denied — without this check,
	// the sub-agent gets zero tools and hallucinates tool calls.
	unavailable := t.subAgentRegistry.UnavailableNames(ctx, req.ToolNames)
	if len(unavailable) == 0 {
		return nil
	}
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.verifyTools", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)
	logr.Warn("create_agent: requested tools are denied or unavailable",
		"unavailable", unavailable,
		"requested", req.ToolNames,
		"agent_name", req.AgentName)
	return fmt.Errorf(
		"cannot create sub-agent %q: the following tools are denied or unavailable: [%s]. "+
			"These tools are blocked by configuration (denied_tools in .genie.toml). "+
			"Re-plan this task using only available tools, or set tool_names to empty to use all allowed tools",
		req.AgentName, strings.Join(unavailable, ", "))
}

func (t *createAgentTool) executeInner(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	if len(req.ToolNames) == 0 {
		return CreateAgentResponse{
			Status: AgentStatusError,
			Output: "No tools specified for sub-agent. Please specify tools in the prompt.",
		}, nil
	}
	if err := t.verifyTools(ctx, req); err != nil {
		return CreateAgentResponse{
			Status: AgentStatusError,
			Output: err.Error(),
		}, nil
	}
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.executeInner", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)
	if err := t.doPreflightChecks(ctx, req); err != nil {
		return CreateAgentResponse{
			Status: AgentStatusError,
			Output: err.Error(),
		}, nil
	}

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
	scopedRegistry := t.subAgentRegistry.Include(req.ToolNames...)

	// Create ephemeral states for dynamic skill loaders so sub-agents
	// have isolated skill environments that don't conflict globally.
	scopedRegistry = scopedRegistry.CloneWithEphemeralProviders()

	// Empty-memory guard: if all requested tools are retrieval-only AND
	// at least one is vector-backed (memory_search), probe the vector
	// store — if it's empty, skip this sub-agent entirely. Prevents
	// burning LLM budget searching a store that has no documents.
	// Graph-only agents are NOT guarded because we can't probe graph
	// emptiness from the vector store.
	if t.isRetrievalOnly(ctx, scopedRegistry) && t.hasVectorBackedTools(ctx, scopedRegistry) && t.isMemoryEmpty(ctx) {
		logr.Info("skipping retrieval-only sub-agent: memory store is empty",
			"agent_name", req.AgentName,
			"tools", scopedRegistry.ToolNames(ctx))
		return CreateAgentResponse{
			Status: AgentStatusSuccess,
			Output: "No relevant data found — the memory store is empty. " +
				"No knowledge base entries exist to search. " +
				"Consider using other tools to gather information.",
		}, nil
	}

	selectedTools := t.toolWrapSvc.Wrap(scopedRegistry.AllTools(ctx), toolwrap.WrapRequest{
		AgentName:     req.AgentName,
		WorkingMemory: t.workingMemory,
	})
	if req.TaskType == "" {
		req.TaskType = modelprovider.TaskPlanning
	}

	modelToUse, err := t.modelProvider.GetModel(ctx, req.TaskType)
	if err != nil {
		return CreateAgentResponse{
			Status: AgentStatusError,
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
		// Default to streaming: newer model APIs (e.g. Anthropic claude-sonnet-4-5)
		// reject non-streaming requests with "streaming is required for operations
		// that may take longer than 10 minutes".
		llmagent.WithGenerationConfig(model.GenerationConfig{Stream: true}),
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

	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, req.timeout())
	defer cancelTimeout()
	logr.Info("sub-agent timeout set", "timeout", req.timeout().String())

	// Wrap with CancelCause so loop-detection and empty-results middlewares
	// can terminate the sub-agent run immediately when they detect futile
	// loops or fruitless search calls.
	cancelCtx, cancelCause := context.WithCancelCause(timeoutCtx)
	defer cancelCause(nil)
	runCtx := toolwrap.WithCancelCause(cancelCtx, cancelCause)

	// Retry transient upstream LLM errors (503 / rate-limit / overloaded)
	// so that temporary provider capacity issues don't fail the sub-agent.
	var evCh <-chan *event.Event
	runErr := retrier.Retry(runCtx, func() error {
		var err error
		prompt := req.Goal
		if req.Context != "" {
			prompt = fmt.Sprintf("Context:\n%s\n\nGoal:\n%s", req.Context, req.Goal)
		}
		evCh, err = r.Run(runCtx, "parent", req.AgentName, model.NewUserMessage(prompt))
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
			Status: AgentStatusError,
			Output: fmt.Sprintf("sub-agent failed to start: %v", runErr),
		}, nil
	}

	// Collect response — accumulate partial output even if errors occur.
	// On context deadline, we keep whatever was gathered instead of losing it.
	// Additionally capture tool result content so that if the sub-agent
	// errors (e.g. budget exceeded) without a final LLM response, the
	// parent agent still receives the knowledge gathered by tool calls.
	var sb strings.Builder
	var toolResultsSB strings.Builder
	const maxToolResultsLen = 16000
	var lastErr string
	timedOut := false
	toolCallCount := 0
	seenToolIDs := make(map[string]struct{})
	usedToolNames := make(map[string]struct{})
	for ev := range evCh {
		if ev.Error != nil {
			lastErr = ev.Error.Message
			// Don't early-return: keep collecting any remaining events.
			// The channel will close shortly and we'll handle the error below.
			logr.Warn("sub-agent event error (continuing to collect output)", "error", lastErr)
			continue
		}
		if ev.Response != nil {
			logr.Debug("sub-agent event", "event", ev)
			for _, choice := range ev.Choices {
				if choice.Message.Role == model.RoleAssistant && choice.Message.Content != "" {
					sb.WriteString(choice.Message.Content)
				}
				// Count unique tool calls via ToolID or ToolCalls array to avoid
				// over-counting when streamed chunks arrive. We check this
				// independently of Content because some models return empty
				// content chunks when streaming tool executions or responses.
				if tid := choice.Message.ToolID; tid != "" {
					if _, seen := seenToolIDs[tid]; !seen {
						seenToolIDs[tid] = struct{}{}
						toolCallCount++
					}
				}
				for _, tc := range choice.Message.ToolCalls {
					if tid := tc.ID; tid != "" {
						if _, seen := seenToolIDs[tid]; !seen {
							seenToolIDs[tid] = struct{}{}
							toolCallCount++
						}
					}
					// Capture tool name for co-occurrence graph.
					if tc.Function.Name != "" {
						usedToolNames[tc.Function.Name] = struct{}{}
					}
				}

				// Capture tool result content as partial findings.
				// Tool results carry the actual data (file contents, search
				// results, etc.) that the sub-agent gathered. When the sub-agent
				// exhausts its budget before producing a final summary, these
				// results are the only record of what was learned.
				if (choice.Message.ToolID != "" || ev.Object == model.ObjectTypeToolResponse) && choice.Message.Content != "" && toolResultsSB.Len() < maxToolResultsLen {
					remaining := maxToolResultsLen - toolResultsSB.Len()
					content := choice.Message.Content
					if len(content) > remaining {
						cut := remaining
						for cut > 0 && !utf8.RuneStart(content[cut]) {
							cut--
						}
						// Append a truncation note so the agent knows data was cut off
						content = content[:cut] + "\n...[Output truncated due to tool-results length limit]..."
					}
					toolResultsSB.WriteString(content)
					// Only write separator if budget remains after the content.
					if sep := "\n---\n"; toolResultsSB.Len()+len(sep) <= maxToolResultsLen {
						toolResultsSB.WriteString(sep)
					}
				}
			}
		}
	}

	// Check if the context was cancelled. Distinguish timeouts from
	// middleware-triggered cancellations (loop detection, empty results)
	// so the output message is accurate.
	if runCtx.Err() != nil {
		cause := context.Cause(runCtx)
		if cause != nil && cause != runCtx.Err() {
			// Middleware-triggered cancel (loop detection or empty results).
			// Not a timeout — the sub-agent was deliberately stopped.
			logr.Warn("sub-agent cancelled by middleware",
				"cause", cause.Error(),
				"partial_output_length", sb.Len())

			// Surface the cancellation cause to the parent agent instead of swallowing it
			lastErr = cause.Error()
		} else {
			timedOut = true
			logr.Warn("sub-agent context expired, returning partial results",
				"error", runCtx.Err().Error(),
				"partial_output_length", sb.Len())
		}
	}

	result := sb.String()

	// When the sub-agent produced no LLM output (e.g. deadline hit mid-generation),
	// check working memory for data stored by tool calls that completed before
	// the deadline (http_request → summarize → memory_store pipeline).
	if result == "" {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		if stored, ok := t.workingMemory.Recall(wmKey); ok && stored != "" {
			result = stored
			logr.Info("recovered sub-agent output from working memory", "key", wmKey, "length", len(result))
		}
	}

	logr.Info("sub-agent execution completed",
		"output_length", len(result), "timed_out", timedOut, "had_error", lastErr != "")

	status, result := req.resolveStatus(timedOut, lastErr, result, toolResultsSB.String())

	// --- Post-execution pipeline ---
	// Each stage is an independently testable method.
	// Convert used tool names set to slice for co-occurrence tracking.
	usedNames := make([]string, 0, len(usedToolNames))
	for name := range usedToolNames {
		usedNames = append(usedNames, name)
	}

	sar := subAgentResult{
		output:        result,
		status:        status,
		toolCallCount: toolCallCount,
		timedOut:      timedOut,
		toolNameList:  toolNameList,
		numTools:      len(selectedTools),
		usedToolNames: usedNames,
	}

	sar = t.applyZeroToolUseGuard(ctx, req, sar)
	sar = t.runHalGuardPostCheck(ctx, req, sar, modelToUse)
	t.storeResults(ctx, req, sar)
	t.recordToolCooccurrence(ctx, sar)
	sar = t.summarizeOutput(ctx, req, sar)

	return CreateAgentResponse{
		Output: sar.output,
		Status: sar.status,
	}, nil
}

func (t *createAgentTool) doPreflightChecks(ctx context.Context, req CreateAgentRequest) error {
	if t.halGuard == nil {
		return nil
	}
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.doPreflightChecks", "goal", toolwrap.TruncateForAudit(req.Goal, 80), "name", req.AgentName)
	// --- Pre-delegation grounding check (P0) ---
	// Validates the goal is grounded in reality before spending any tokens.
	// Catches fabricated scenarios (role-play, hypothetical incidents) that
	// would waste budget and inject hallucinations into the parent context.
	// The confidence score (0–1) lets us tune the sensitivity threshold.
	preTracer := otel.Tracer("genie")
	preCtx, preSpan := preTracer.Start(ctx, "halguard.PreCheck")
	preResult, preErr := t.halGuard.PreCheck(preCtx, halguard.PreCheckRequest{
		Goal:      req.Goal,
		Context:   req.Context,
		ToolNames: req.ToolNames,
	})
	if preErr != nil {
		preSpan.RecordError(preErr)
		preSpan.SetStatus(codes.Error, preErr.Error())
		preSpan.End()
		logr.Warn("halguard pre-check error, proceeding anyway", "error", preErr)
		return nil
	}
	preSpan.SetAttributes(
		attribute.Float64("halguard.precheck.confidence", preResult.Confidence),
		attribute.String("halguard.precheck.summary", preResult.Summary),
		attribute.String("halguard.precheck.signals", preResult.Signals.String()),
		attribute.String("halguard.agent_name", req.AgentName),
	)

	// Use configured pre-check threshold; fall back to 0.4 default.
	threshold := 0.4
	if t.halGuardThreshold > 0 {
		threshold = t.halGuardThreshold
	}

	if preResult.Confidence < threshold {
		preSpan.SetAttributes(
			attribute.Bool("halguard.precheck.blocked", true),
			attribute.Float64("halguard.precheck.threshold", threshold),
		)
		preSpan.SetStatus(codes.Error, "grounding check failed")
		preSpan.End()
		logr.Warn("halguard pre-check: low confidence, blocking sub-agent",
			"confidence", preResult.Confidence,
			"signals", preResult.Signals,
			"summary", preResult.Summary)
		return fmt.Errorf("grounding check failed (confidence=%.2f): %s. "+
			"The goal appears to describe a fabricated scenario. "+
			"Please rephrase with a real, actionable task",
			preResult.Confidence, preResult.Summary)
	}
	preSpan.SetAttributes(
		attribute.Bool("halguard.precheck.blocked", false),
	)
	preSpan.SetStatus(codes.Ok, "")
	preSpan.End()
	logr.Debug("halguard pre-check passed",
		"confidence", preResult.Confidence)
	return nil
}

// subAgentResult carries the output, status, and metadata through the
// post-execution pipeline stages. Each stage reads and returns a
// (possibly modified) copy, keeping the pipeline composable and each
// stage independently testable.
type subAgentResult struct {
	output        string
	status        AgentStatus
	toolCallCount int
	timedOut      bool
	toolNameList  []string
	numTools      int
	usedToolNames []string // Tools actually invoked (for co-occurrence graph)
}

// applyZeroToolUseGuard detects when a sub-agent had action tools but made
// zero tool calls, producing text-only output. This indicates the sub-agent
// echoed commands as text or refused the task. It annotates the output and
// sets status to "tool_use_failure" so the caller can auto-retry.
func (t *createAgentTool) applyZeroToolUseGuard(_ context.Context, _ CreateAgentRequest, sar subAgentResult) subAgentResult {
	if sar.toolCallCount == 0 && sar.output != "" && sar.status != AgentStatusError && !sar.timedOut && sar.numTools > 0 {
		sar.output = fmt.Sprintf(
			"⚠️ SUB-AGENT DID NOT USE TOOLS: The sub-agent produced a text-only response "+
				"without calling any of its available tools (%s). This likely means it echoed "+
				"commands as text or refused the task instead of executing it. "+
				"The sub-agent should be re-spawned. Original output follows:\n\n%s",
			strings.Join(sar.toolNameList, ", "), sar.output)
		sar.status = AgentStatusToolUseFailure
	}
	return sar
}

// runHalGuardPostCheck verifies sub-agent output for hallucinations using
// cross-model consistency. Only runs when halGuard is configured and the
// sub-agent completed successfully with actual output. Skips timed-out,
// budget-exceeded, and tool_use_failure statuses because those produce
// boilerplate text without meaningful factual content to verify.
func (t *createAgentTool) runHalGuardPostCheck(ctx context.Context, req CreateAgentRequest, sar subAgentResult, modelToUse modelprovider.ModelMap) subAgentResult {
	if t.halGuard == nil || sar.output == "" || sar.status != AgentStatusSuccess {
		return sar
	}

	logr := logger.GetLogger(ctx).With("fn", "runHalGuardPostCheck", "name", req.AgentName)
	postTracer := otel.Tracer("genie")
	postCtx, postSpan := postTracer.Start(ctx, "halguard.PostCheck")
	postSpan.SetAttributes(
		attribute.String("halguard.agent_name", req.AgentName),
		attribute.Int("halguard.postcheck.output_len", len(sar.output)),
		attribute.Int("halguard.postcheck.tool_calls", sar.toolCallCount),
	)
	vr, verifyErr := t.halGuard.PostCheck(postCtx, halguard.PostCheckRequest{
		Goal:            req.Goal,
		Context:         req.Context,
		Output:          sar.output,
		ToolCallsMade:   sar.toolCallCount,
		ToolSummary:     strings.Join(sar.toolNameList, ", "),
		GenerationModel: modelToUse,
	})
	if verifyErr != nil {
		postSpan.RecordError(verifyErr)
		postSpan.SetStatus(codes.Error, verifyErr.Error())
		postSpan.End()
		logr.Warn("halguard post-check failed, using unverified output", "error", verifyErr)
	} else if !vr.IsFactual {
		postSpan.SetAttributes(
			attribute.String("halguard.postcheck.tier", string(vr.Tier)),
			attribute.Bool("halguard.postcheck.is_factual", false),
			attribute.Int("halguard.postcheck.contradictions", len(vr.BlockScores)),
		)
		postSpan.SetStatus(codes.Error, "hallucination detected")
		postSpan.End()
		logr.Warn("halguard detected hallucination in sub-agent output",
			"tier", vr.Tier, "contradictions", len(vr.BlockScores))
		sar.output = vr.CorrectedText
		sar.status = AgentStatusVerifiedCorrected
	} else {
		postSpan.SetAttributes(
			attribute.String("halguard.postcheck.tier", string(vr.Tier)),
			attribute.Bool("halguard.postcheck.is_factual", true),
		)
		postSpan.SetStatus(codes.Ok, "")
		postSpan.End()
		logr.Info("halguard post-check passed", "tier", vr.Tier)
	}
	return sar
}

// storeResults persists the sub-agent result into working memory and
// episodic memory for future retrieval by the parent agent and similar
// future goals respectively. Results are stored regardless of status so
// that partial findings from failed/timed-out sub-agents are preserved
// in shared memory and available to sibling or follow-up agents.
func (t *createAgentTool) storeResults(ctx context.Context, req CreateAgentRequest, sar subAgentResult) {
	logr := logger.GetLogger(ctx).With("fn", "storeResults", "name", req.AgentName)

	// Store sub-agent result in working memory so the parent can retrieve
	// it via memory_search and compose a single user-facing message.
	// Store even partial/error results — the parent agent can still
	// extract useful information from incomplete findings.
	if sar.output != "" {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		t.workingMemory.Store(wmKey, sar.output)
		logr.Info("sub-agent result stored in working memory", "key", wmKey, "length", len(sar.output), "status", sar.status)
	}

	// Store result as an episode for future in-context retrieval.
	// Paper Section 4.2: episodic memory stores subgoal-level experiences
	// so future agent nodes with similar goals get relevant examples.
	// Store both successful and failed episodes — failure episodes help
	// future agents avoid repeating the same mistakes or strategies.
	if t.episodic != nil && sar.output != "" {
		trajectory := sar.output
		const maxTrajectoryRunes = 500
		runes := []rune(trajectory)
		if len(runes) > maxTrajectoryRunes {
			trajectory = string(runes[:maxTrajectoryRunes]) + "... (truncated)"
		}
		episodeStatus := memory.EpisodeSuccess
		if sar.timedOut || sar.status == AgentStatusError || sar.status == AgentStatusPartial {
			episodeStatus = memory.EpisodeFailure
		}

		// For failure episodes, generate a verbal reflection and importance
		// score — matching the agent_node.go pattern. Without this, failures
		// are stored as raw trajectories that don't surface actionable lessons.
		var reflection string
		var importance int
		if episodeStatus == memory.EpisodeFailure {
			reflection = generateFailureReflection(ctx, req.Goal, trajectory, t.failureReflector)
			scoringInput := reflection
			if scoringInput == "" {
				scoringInput = trajectory
			}
			importance = scoreEpisodeImportance(ctx, t.importanceScorer, req.Goal, scoringInput, episodeStatus)
		} else {
			importance = scoreEpisodeImportance(ctx, t.importanceScorer, req.Goal, trajectory, episodeStatus)
		}

		t.episodic.Store(ctx, memory.Episode{
			Goal:       req.Goal,
			Trajectory: trajectory,
			Status:     episodeStatus,
			Reflection: reflection,
			Importance: importance,
		})
		logr.Info("sub-agent result stored in episodic memory",
			"goal", toolwrap.TruncateForAudit(req.Goal, 60),
			"episode_status", episodeStatus,
			"has_reflection", reflection != "",
			"importance", importance,
		)
	}
}

// recordToolCooccurrence feeds the tools actually used by a sub-agent into
// the co-occurrence graph so future tool recommendations are context-aware.
// Only records when >= 2 tools were used (co-occurrence requires pairs).
func (t *createAgentTool) recordToolCooccurrence(ctx context.Context, sar subAgentResult) {
	if t.toolIndex == nil || len(sar.usedToolNames) < 2 {
		return
	}
	t.toolIndex.RecordToolUsage(ctx, sar.usedToolNames)
}

// summarizeOutput compresses large sub-agent output when the caller
// explicitly opted in via SummarizeOutput. Skips summarization if
// the sub-agent timed out (the summarizer would likely also fail).
func (t *createAgentTool) summarizeOutput(ctx context.Context, req CreateAgentRequest, sar subAgentResult) subAgentResult {
	if !req.SummarizeOutput || sar.timedOut || t.summarizer == nil || len(sar.output) <= summarizeThreshold {
		return sar
	}

	logr := logger.GetLogger(ctx).With("fn", "summarizeOutput", "name", req.AgentName)
	logr.Info("summarizing large sub-agent output", "original_length", len(sar.output), "threshold", summarizeThreshold)
	summarized, err := t.summarizer.Summarize(ctx, agentutils.SummarizeRequest{
		Content:              sar.output,
		RequiredOutputFormat: agentutils.OutputFormatMarkdown,
	})
	if err == nil {
		logr.Info("sub-agent output summarized", "original_length", len(sar.output), "summarized_length", len(summarized))
		sar.output = summarized
	} else {
		logr.Warn("sub-agent output summarization failed, using original", "error", err)
	}
	return sar
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

	// Inject the parent's top-level goal and context into every step.
	// This ensures that when the planner sets an overarching persona, AWS credentials,
	// or environment context in the top-level goal (e.g., "You are an AWS Copilot..."),
	// the individual step agents aren't spawned completely blind to that context.
	parentCtx := req.Context
	if parentCtx == "" {
		parentCtx = "Top-level plan objective constraints: " + req.Goal
	} else {
		parentCtx = fmt.Sprintf("Top-level plan objective constraints: %s\n\nAdditional Context:\n%s", req.Goal, req.Context)
	}

	// --- Pre-planning wisdom consultation ---
	// Consult episodic memory and wisdom BEFORE executing the plan so each
	// step's context is enriched with relevant past successes and failures.
	// This closes the gap where plan decomposition was "blind" to history.
	var advisoryResult memory.PlanAdvisoryResult
	if t.planAdvisor != nil {
		stepGoals := make(map[string]string, len(req.Steps))
		for _, step := range req.Steps {
			stepGoals[step.Name] = step.Goal
		}
		advisoryResult = t.planAdvisor.Advise(ctx, memory.PlanAdvisoryRequest{
			OverallGoal: req.Goal,
			StepGoals:   stepGoals,
		})
		logr.Info("pre-planning advisory complete",
			"steps_advised", advisoryResult.StepsAdvised(),
			"total_steps", len(req.Steps),
		)
	}

	for i := range req.Steps {
		if req.Steps[i].Context != "" {
			req.Steps[i].Context = parentCtx + "\n\nStep Context:\n" + req.Steps[i].Context
		} else {
			req.Steps[i].Context = parentCtx
		}

		// Append advisory from past experiences if available.
		advisory := advisoryResult.ForStep(req.Steps[i].Name)
		if advisory != "" {
			req.Steps[i].Context += advisory
		}
	}

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
		ToolRegistry:  t.subAgentRegistry,
		ToolWrapSvc:   t.toolWrapSvc,
		WrapRequest: toolwrap.WrapRequest{
			AgentName:     req.AgentName,
			WorkingMemory: t.workingMemory,
		},
		ModelProvider: t.modelProvider,
	})
	if err != nil {
		return CreateAgentResponse{
			Status: AgentStatusError,
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
	if output != "" {
		wmKey := fmt.Sprintf("subagent:%s:result", req.AgentName)
		t.workingMemory.Store(wmKey, output)
		logr.Info("plan result stored in working memory", "key", wmKey, "length", len(output))
	}

	status := AgentStatusSuccess
	if result.Status != Success {
		status = AgentStatusPartial
	}

	// When output is empty, provide rich context so the parent agent can
	// adapt its retry strategy instead of re-running the same failing plan.
	if output == "" && status != AgentStatusSuccess {
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
	if status == AgentStatusPartial && !strings.HasPrefix(output, "⚠️") {
		output = "⚠️ [Partial results] " + output
	}

	logr.Info("plan execution completed", "status", status, "output_length", len(output),
		"succeeded", succeeded, "failed", failed)

	return CreateAgentResponse{
		Output: output,
		Status: status,
	}, nil
}

// vectorBackedTools is the subset of retrieval tools backed by the vector
// store. The isMemoryEmpty guard only applies when the sub-agent uses
// these tools. Graph-only agents are not guarded because the vector store
// probe cannot determine graph store emptiness.
var vectorBackedTools = map[string]bool{
	vector.MemorySearchToolName: true,
}

// isRetrievalOnly returns true when every tool in the registry is a
// retrieval-only tool (per toolwrap.IsRetrievalTool). Sub-agents with only
// these tools are candidates for the empty-memory guard.
func (t *createAgentTool) isRetrievalOnly(ctx context.Context, registry *tools.Registry) bool {
	names := registry.ToolNames(ctx)
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		if !toolwrap.IsRetrievalTool(name) {
			return false
		}
	}
	return true
}

// hasVectorBackedTools returns true if any tool in the registry is backed
// by the vector store. Only these tools can be short-circuited by the
// isMemoryEmpty probe.
func (t *createAgentTool) hasVectorBackedTools(ctx context.Context, registry *tools.Registry) bool {
	for _, name := range registry.ToolNames(ctx) {
		if vectorBackedTools[name] {
			return true
		}
	}
	return false
}

// isMemoryEmpty probes the vector store with a lightweight search to check
// whether it likely contains any documents. Returns true if the store is
// nil or if a best-effort probe returns zero results.
//
// Uses a short sentinel query ("memory") instead of an empty string because
// SearchWithFilter rejects empty queries without filters. The sentinel is
// still embedded and run through vector similarity search; in practice a
// non-empty store should return at least one nearest neighbor. Without this
// check, retrieval-only sub-agents would burn their entire LLM call budget
// rephrasing fruitless queries when the store is empty.
func (t *createAgentTool) isMemoryEmpty(ctx context.Context) bool {
	if t.vectorStore == nil {
		return true
	}
	results, err := t.vectorStore.Search(ctx, vector.SearchRequest{Query: "memory", Limit: 1})
	if err != nil {
		// If the probe fails, assume memory is non-empty to avoid
		// false positives — better to run a possibly-futile sub-agent
		// than to silently skip a sub-agent that might succeed.
		return false
	}
	return len(results) == 0
}

// AuditEventSubAgentCreated is the audit event type for sub-agent creation.
const AuditEventSubAgentCreated audit.EventType = "subagent_created"

// auditSubAgentCreation emits a durable audit event when a sub-agent is created.
// This records the delegation of authority: which tools were given, the goal,
// task type, and budget parameters.
func (t *createAgentTool) auditSubAgentCreation(ctx context.Context, req CreateAgentRequest) {
	if t.auditor == nil {
		return
	}
	metadata := map[string]any{
		"agent_name":          req.AgentName,
		"goal":                req.Goal,
		"tool_names":          req.ToolNames,
		"task_type":           string(req.TaskType),
		"max_tool_iterations": req.clampedMaxToolIterations(),
		"max_llm_calls":       req.clampedMaxLLMCalls(),
		"timeout_seconds":     req.timeout().Seconds(),
		"step_count":          len(req.Steps),
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
	}
	t.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventSubAgentCreated,
		Actor:     "orchestrator",
		Action:    "create_agent",
		Metadata:  metadata,
	})
}
