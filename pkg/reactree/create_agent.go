package reactree

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// CreateAgentRequest is the input for the create_agent tool.
type CreateAgentRequest struct {
	Goal      string                 `json:"goal" jsonschema:"description=The goal or task for the sub-agent to accomplish,required"`
	ToolNames []string               `json:"tool_names,omitempty" jsonschema:"description=Names of tools to give the sub-agent. If empty all tools are provided."`
	TaskType  modelprovider.TaskType `json:"task_type,omitempty" jsonschema:"description=Type of task for the sub-agent to accomplish, Should be one of efficiency/long_horizon_autonomy/mathematical/general_task/novel_reasoning/scientific_reasoning/terminal_calling/planning,required"`
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
	toolRegistry  map[string]tool.Tool
}

// NewCreateAgentTool creates a tool that spawns sub-agents with dynamic tool
// subsets. The llmModel is the LLM to use for sub-agents. The toolRegistry is
// a name→tool map of all available tools the sub-agent can choose from.
func NewCreateAgentTool(modelProvider modelprovider.ModelProvider, toolRegistry map[string]tool.Tool) tool.Tool {
	// Build description listing available tools
	var toolList []string
	for name := range toolRegistry {
		toolList = append(toolList, name)
	}

	t := &createAgentTool{
		modelProvider: modelProvider,
		toolRegistry:  toolRegistry,
	}

	return function.NewFunctionTool(
		t.execute,
		function.WithName("create_agent"),
		function.WithDescription(fmt.Sprintf(
			"Spawn a focused sub-agent to accomplish a specific goal with a selected "+
				"subset of tools. Best for WRITE-HEAVY or MULTI-STEP tasks (editing files, "+
				"build-test-fix loops, shell workflows). For pure file reads, use read tools directly.\n\n"+
				"TASK TYPE GUIDANCE (maps to different models for cost/speed optimization):\n"+
				"- tool_calling: Best for file edits, shell commands (fastest, cheapest)\n"+
				"- planning: Best for complex reasoning, architecture decisions (most capable)\n"+
				"- terminal_calling: Best for shell/CLI-heavy tasks\n"+
				"- novel_reasoning: Best for creative problem-solving\n\n"+
				"BEST PRACTICES:\n"+
				"- Always set task_type to 'tool_calling' for file/shell sub-tasks\n"+
				"- Give only the tools the sub-agent needs (minimal tool set)\n"+
				"- Batch related work into one sub-agent goal\n"+
				"- For independent sub-tasks, call create_agent multiple times in parallel\n"+
				"- Do NOT delegate pure file reads — sub-agents pay the same input token cost\n\n"+
				"Available tools: %s",
			strings.Join(toolList, ", "),
		)),
	)
}

func (t *createAgentTool) execute(ctx context.Context, req CreateAgentRequest) (CreateAgentResponse, error) {
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

	// Create a fresh sub-agent with only the selected tools
	subAgent := llmagent.New(
		"sub-agent",
		llmagent.WithModel(modelToUse),
		llmagent.WithTools(selectedTools),
		llmagent.WithInstruction("You are a focused sub-agent. Complete the given task using ONLY your available tools. "+
			"Be concise — return the essential result without commentary. "+
			"If working memory already contains relevant data, use it instead of re-reading files. "+
			"ERROR BUDGET: If a command or tool call fails 3 times consecutively, stop and report the failure rather than retrying."),
		llmagent.WithDescription("Focused sub-agent for delegated tasks"),
		llmagent.WithMaxLLMCalls(10),
		llmagent.WithMaxToolIterations(8),
	)

	// Run via a one-shot runner with isolated session
	r := runner.NewRunner(
		"sub-agent",
		subAgent,
		runner.WithSessionService(inmemory.NewSessionService()),
	)

	evCh, err := r.Run(ctx, "parent", "sub-session", model.NewUserMessage(req.Goal))
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

	return CreateAgentResponse{
		Output: sb.String(),
		Status: "success",
	}, nil
}
