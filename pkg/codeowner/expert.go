package codeowner

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/reactree"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
)

//go:embed prompts/persona.txt
var persona string

type CodeQuestion struct {
	Question  string
	EventChan chan<- interface{}
	OutputDir string
}

// ToolDeps holds pre-initialized optional tools injected from the application layer.
// Each field is optional — nil or empty means "not configured".
type ToolDeps struct {
	WebSearch   tool.Tool     // from websearch.NewTool()
	MCPTools    []tool.Tool   // from mcp.Client.GetTools()
	SkillTools  []tool.Tool   // from skills.CreateSkillTools()
	VectorStore *vector.Store // generates memory_store + memory_search tools
}

//go:generate go tool counterfeiter -generate

// CodeOwner is an expert that can chat about the codebase
//
//counterfeiter:generate . CodeOwner
type CodeOwner interface {
	Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error
	Close() error
}

type codeOwner struct {
	expert        expert.Expert
	workingMemory *rtmemory.WorkingMemory
	treeExecutor  reactree.TreeExecutor
	memorySvc     memory.Service
	memoryUserKey memory.UserKey
	tools         []tool.Tool
}

// NewcodeOwner creates a new codeOwner with an integrated ReAcTree executor.
// The working memory persists across chat turns, allowing the agent to share
// observations from previous interactions. The tree executor enables
// hierarchical task decomposition for complex queries when activated.
func NewcodeOwner(
	ctx context.Context,
	modelProvider modelprovider.ModelProvider,
	workingDirectory string,
	deps ToolDeps,
) (CodeOwner, error) {
	// Define Persona
	expertBio := expert.ExpertBio{
		Personality: langfuse.GetPrompt(ctx, "genie_codeowner_persona", persona),
		Name:        "code-owner",
		Description: "Code Owner that knows the in and out about the codebase",
	}

	exp, err := expertBio.ToExpert(ctx, modelProvider)
	if err != nil {
		return nil, err
	}

	// Initialize shared working memory for cross-turn observation sharing
	wm := rtmemory.NewWorkingMemory()

	// Initialize the ReAcTree executor for hierarchical task decomposition
	episodicMemoryCfg := rtmemory.DefaultEpisodicMemoryConfig()
	treeExec := reactree.NewTreeExecutor(exp, wm, episodicMemoryCfg.NewServiceEpisodicMemory(), reactree.DefaultTreeConfig())

	// Initialize memory.Service for conversation history persistence.
	// Each Q&A turn is stored and retrieved by semantic similarity,
	// enabling the expert to recall past discussions when asked.
	memorySvc := inmemory.NewMemoryService()
	memoryUserKey := memory.UserKey{
		AppName: "genie-codeowner",
		UserID:  "default",
	}

	// Initialize file tools scoped to the working directory.
	var tools []tool.Tool
	if workingDirectory != "" {
		if ts, err := file.NewToolSet(file.WithBaseDir(workingDirectory)); err == nil {
			tools = ts.Tools(ctx)
		}

		// Initialize local code executor for shell access (bash only for now)
		// This enables the agent to run verification commands like 'go test' or 'terraform validate'.
		exec := local.New(local.WithWorkDir(workingDirectory))

		// Use ShellTool which wraps the code executor
		codeTool := NewShellTool(exec)
		tools = append(tools, codeTool)
	}

	// Append optional tools from ToolDeps
	if deps.WebSearch != nil {
		tools = append(tools, deps.WebSearch)
	}
	tools = append(tools, deps.MCPTools...)
	tools = append(tools, deps.SkillTools...)
	if deps.VectorStore != nil {
		tools = append(tools, vector.NewMemoryStoreTool(deps.VectorStore))
		tools = append(tools, vector.NewMemorySearchTool(deps.VectorStore))
	}

	// Create the create_agent tool for dynamic sub-agent spawning.
	// The parent LLM picks which tools to give each sub-agent at call time.
	toolRegistry := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		toolRegistry[t.Declaration().Name] = t
	}
	tools = append(tools, reactree.NewCreateAgentTool(modelProvider, toolRegistry))

	return &codeOwner{
		expert:        exp,
		workingMemory: wm,
		treeExecutor:  treeExec,
		memorySvc:     memorySvc,
		memoryUserKey: memoryUserKey,
		tools:         tools,
	}, nil
}

// Close releases resources held by the codeOwner, including the conversation
// history memory service. Callers should defer Close() after NewcodeOwner.
func (c *codeOwner) Close() error {
	if c.memorySvc != nil {
		return c.memorySvc.Close()
	}
	return nil
}

func (c *codeOwner) Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error {
	_ = logger.GetLogger(ctx).With("fn", "codeExpert.Chat")

	// Retrieve relevant past conversation turns from memory.
	pastContext := c.recallConversation(ctx, req.Question)

	// Build the message with past conversation context injected
	message := req.Question
	if pastContext != "" {
		message = fmt.Sprintf(
			"## Relevant Past Conversations\n%s\n\n## Current Question\n%s",
			pastContext, req.Question,
		)
	}

	// Execute using ReAcTree for structured reasoning and task decomposition
	res, err := c.treeExecutor.Run(ctx, reactree.TreeRequest{
		Goal:      message,
		EventChan: req.EventChan,
		Tools:     c.tools,
	})

	if err != nil {
		return err
	}

	// Send the final result to the output channel
	outputChan <- res.Output

	// Store the conversation turn in memory for future recall (best-effort).
	c.storeConversation(ctx, req.Question, res.Output)

	return nil
}

// recallConversation searches memory.Service for past turns relevant
// to the current question and formats them as context.
func (c *codeOwner) recallConversation(ctx context.Context, question string) string {
	entries, err := c.memorySvc.SearchMemories(ctx, c.memoryUserKey, question)
	if err != nil || len(entries) == 0 {
		return ""
	}

	// Limit to top 3 most relevant past turns
	limit := 3
	if len(entries) < limit {
		limit = len(entries)
	}

	var sb strings.Builder
	for i := 0; i < limit; i++ {
		if entries[i] == nil || entries[i].Memory == nil {
			continue
		}
		sb.WriteString(entries[i].Memory.Memory)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// storeConversation persists a Q&A turn into memory.Service.
func (c *codeOwner) storeConversation(ctx context.Context, question, answer string) {
	// Format the turn as a structured summary for retrieval
	summary := fmt.Sprintf("Q: %s\nA: %s", question, truncate(answer, 500))
	topics := []string{"conversation", "chat-turn"}

	_ = c.memorySvc.AddMemory(ctx, c.memoryUserKey, summary, topics)
}

// truncate shortens a string to the given max length (in runes), appending "..." if truncated.
// Using rune-based slicing ensures multi-byte UTF-8 characters are not split.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	const suffix = "..."
	if maxLen > len(suffix) {
		return string(runes[:maxLen-len(suffix)]) + suffix
	}
	return string(runes[:maxLen])
}
