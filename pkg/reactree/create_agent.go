package reactree

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/agentutils"
	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	"github.com/appcd-dev/go-lib/logger"
	"go.opentelemetry.io/otel"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// CreateAgentRequest is the input for the create_agent tool.
type CreateAgentRequest struct {
	AgentName         string                 `json:"agent_name" jsonschema:"description=Name of the sub-agent,required"`
	Goal              string                 `json:"goal" jsonschema:"description=The goal or task for the sub-agent to accomplish,required"`
	ToolNames         []string               `json:"tool_names,omitempty" jsonschema:"description=Names of tools to give the sub-agent. If empty all tools are provided."`
	TaskType          modelprovider.TaskType `json:"task_type,omitempty" jsonschema:"description=Type of task for the sub-agent to accomplish, Should be one of efficiency/long_horizon_autonomy/mathematical/general_task/novel_reasoning/scientific_reasoning/terminal_calling/planning,required"`
	MaxToolIterations int                    `json:"max_tool_iterations,omitempty" jsonschema:"description=Maximum number of tool iterations for the sub-agent,required"`
	MaxLLMCalls       int                    `json:"max_llm_calls,omitempty" jsonschema:"description=Maximum number of LLM calls for the sub-agent,required"`
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
	summarizer    agentutils.Summarizer
	toolRegistry  map[string]tool.Tool
	// toolWrapSvc wraps sub-agent tools with HITL approval, audit logging,
	// and caching. When nil, sub-agent tools execute without HITL gating.
	toolWrapSvc   *toolwrap.Service
	workingMemory *memory.WorkingMemory
}

// NewCreateAgentTool creates a tool that spawns sub-agents with dynamic tool
// subsets. The llmModel is the LLM to use for sub-agents. The toolRegistry is
// a name→tool map of all available tools the sub-agent can choose from.
// The optional toolWrapSvc, when provided, wraps sub-agent tools with HITL
// approval gating, audit logging, and file-read caching — ensuring sub-agents
// cannot execute write tools without human approval.
func NewCreateAgentTool(
	modelProvider modelprovider.ModelProvider,
	summarizer agentutils.Summarizer,
	toolRegistry ToolRegistry,
	workingMemory *memory.WorkingMemory,
	toolWrapSvc *toolwrap.Service) tool.Tool {
	// Build description listing available tools
	var toolList []string
	for name := range toolRegistry {
		toolList = append(toolList, name)
	}

	t := &createAgentTool{
		modelProvider: modelProvider,
		summarizer:    summarizer,
		toolRegistry:  toolRegistry,
		toolWrapSvc:   toolWrapSvc,
		workingMemory: workingMemory,
	}

	return function.NewFunctionTool(
		t.execute,
		function.WithName("create_agent"),
		function.WithDescription(fmt.Sprintf(
			"Spawn a sub-agent with selected tools for multi-step tasks. "+
				"task_type: tool_calling (file/shell, fastest), planning (reasoning), "+
				"terminal_calling (CLI), novel_reasoning (creative). "+
				"Give only needed tools. Batch related work into one agent; "+
				"spawn parallel agents for independent tasks.\n\n"+
				"Available tools: %s",
			strings.Join(toolList, ", "),
		)),
	)
}

func (t *createAgentTool) execute(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "createAgentTool.execute", "goal", toolwrap.TruncateForAudit(req.Goal, 80))
	logr.Info("create_agent invoked", "tool_names", req.ToolNames, "task_type", req.TaskType)

	// Select tools from registry
	var selectedTools []tool.Tool
	if len(req.ToolNames) == 0 {
		for _, tl := range t.toolRegistry {
			selectedTools = append(selectedTools, tl)
		}
	} else {
		for _, name := range req.ToolNames {
			if tl, ok := t.toolRegistry[name]; ok {
				selectedTools = append(selectedTools, tl)
			}
		}
	}

	logr.Info("sub-agent tools selected", "count", len(selectedTools))

	// Wrap sub-agent tools with HITL approval, audit logging, and caching.
	// This ensures every sub-agent tool call (run_shell, save_file, etc.)
	// goes through the same approval gate as parent-agent tools.
	// Extract per-request fields (EventChan, ThreadID, RunID) from context
	// so HITL approval events propagate to the UI correctly.
	if t.toolWrapSvc != nil {
		threadID := agui.ThreadIDFromContext(ctx)
		runID := agui.RunIDFromContext(ctx)
		evChan := agui.EventChanFromContext(ctx)
		logr.Info("wrapping sub-agent tools with HITL",
			"threadID", threadID,
			"runID", runID,
			"hasEventChan", evChan != nil,
			"hasApprovalStore", t.toolWrapSvc.ApprovalStore != nil,
		)
		selectedTools = t.toolWrapSvc.Wrap(selectedTools, toolwrap.WrapRequest{
			EventChan: evChan,
			ThreadID:  threadID,
			RunID:     runID,
		})
	}

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
	if req.MaxToolIterations == 0 {
		req.MaxToolIterations = 14
	}
	if req.MaxLLMCalls == 0 {
		req.MaxLLMCalls = 12
	}

	// Base instruction
	instruction := "You are a focused sub-agent. Complete the given task using ONLY your available tools. " +
		"Be concise — return the essential result without commentary. " +
		"If working memory already contains relevant data, use it instead of re-reading files. " +
		"IMPORTANT: save_file, read_file, and list_file only accept RELATIVE paths under the workspace directory. " +
		"Do NOT pass absolute paths (e.g. /tmp/foo) to these tools — use run_shell instead for absolute paths. " +
		"Do not rewrite the same file multiple times unless fixing an error. Write files once and move to the next task. " +
		"ERROR BUDGET: If a command or tool call fails 3 times consecutively, stop and report the failure rather than retrying."

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
	)

	// Run via a one-shot runner with isolated session
	r := runner.NewRunner(
		req.AgentName,
		subAgent,
		runner.WithSessionService(inmemory.NewSessionService()),
	)

	// Explicitly start a span for the sub-agent execution to ensure proper
	// nesting in traces. The tool invoker might not have propagated the
	// tool span context correctly to the runner otherwise.
	tracer := otel.Tracer("genie")
	runCtx, span := tracer.Start(ctx, "sub-agent execution")
	defer span.End()

	evCh, err := r.Run(runCtx, "parent", "sub-session", model.NewUserMessage(req.Goal))
	if err != nil {
		return CreateAgentResponse{
			Status: "error",
			Output: fmt.Sprintf("sub-agent failed to start: %v", err),
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

	// Summarize large sub-agent output to keep context concise for the parent agent.
	const summarizeThreshold = 2000
	if t.summarizer != nil && len(result) > summarizeThreshold {
		logr.Info("summarizing large sub-agent output", "original_length", len(result), "threshold", summarizeThreshold)
		summarized, err := t.summarizer.Summarize(ctx, agentutils.SummarizeRequest{
			Content:              result,
			RequiredOutputFormat: agentutils.OutputFormatText,
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
		Status: "success",
	}, nil
}
