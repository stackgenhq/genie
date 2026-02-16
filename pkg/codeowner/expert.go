package codeowner

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/agentutils"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/osutils"
	"github.com/appcd-dev/genie/pkg/reactree"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
)

//go:embed prompts/persona.txt
var persona string

//go:embed prompts/classify.txt
var classifyPrompt string

type CodeQuestion struct {
	Question  string
	EventChan chan<- interface{}
	OutputDir string

	// SenderContext identifies who sent this message and from where.
	// Format: "platform:senderID:channelID" (e.g. "slack:U12345:C67890")
	// or "tui:local" for terminal input. Used for prompt context and
	// memory isolation (each unique sender gets separate conversation history).
	SenderContext string

	// ExcludeTools lists tool names to omit for this chat turn.
	// For messenger-originated messages, "send_message" is excluded
	// because the chat loop handles the reply directly.
	ExcludeTools []string

	// BrowserTab is an optional context for a specific browser tab.
	// If provided, browser tools will use this context.
	BrowserTab context.Context
}

//go:generate go tool counterfeiter -generate

// CodeOwner is an expert that can chat about the codebase
//
//counterfeiter:generate . CodeOwner
type CodeOwner interface {
	Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error
	Close() error
	Resume() string
}

// requestCategory represents the front desk classification result.
type requestCategory string

const (
	categoryRefuse     requestCategory = "REFUSE"
	categorySalutation requestCategory = "SALUTATION"
	categoryComplex    requestCategory = "COMPLEX"
)

type codeOwner struct {
	expert          expert.Expert
	frontDeskExpert expert.Expert
	workingMemory   *rtmemory.WorkingMemory
	treeExecutor    reactree.TreeExecutor
	memorySvc       memory.Service
	memoryUserKey   memory.UserKey
	tools           reactree.ToolRegistry
	auditor         audit.Auditor
	resume          string
}

// Resume returns a natural language description of the agent's capabilities.
func (c *codeOwner) Resume() string {
	return c.resume
}

// NewCodeOwner creates a new codeOwner with an integrated ReAcTree executor.
// The working memory persists across chat turns, allowing the agent to share
// observations from previous interactions. The tree executor enables
// hierarchical task decomposition for complex queries when activated.
// The approvalStore enables HITL approval gating for sub-agent tool calls;
// when nil, sub-agents execute tools without requiring human approval.
func NewCodeOwner(
	ctx context.Context,
	modelProvider modelprovider.ModelProvider,
	workingDirectory string,
	tools []tool.Tool,
	vectorStore *vector.Store,
	auditor audit.Auditor,
	approvalStore hitl.ApprovalStore,
	memorySvc memory.Service,
) (CodeOwner, error) {
	// Build the persona prompt, appending project-level coding standards if available.
	fullPersona := langfuse.GetPrompt(ctx, "genie_codeowner_persona", persona)
	if agentsGuide := loadAgentsGuide(workingDirectory); agentsGuide != "" {
		fullPersona += "\n\n## Project Standards (from Agents.md)\n\n" + agentsGuide
		logger.GetLogger(ctx).Info("Agents.md loaded and appended to persona")
	} else {
		logger.GetLogger(ctx).Debug("Agents.md not found or empty")
	}

	expertBio := expert.ExpertBio{
		Personality: fullPersona,
		Name:        "genie",
		Description: "Genie — personal AI assistant that gets things done",
	}

	exp, err := expertBio.ToExpert(ctx, modelProvider, &toolwrap.Service{
		Auditor:       auditor,
		ApprovalStore: approvalStore,
	})
	if err != nil {
		return nil, err
	}
	logger.GetLogger(ctx).Info("Expert bio created", "name", expertBio.Name)

	// Create a lightweight front desk expert for request classification.
	// Uses TaskFrontDesk which maps to a fast, cheap model (e.g. gemini-3-flash).
	frontDeskBio := expert.ExpertBio{
		Personality: classifyPrompt,
		Name:        "front-desk",
		Description: "Classifies incoming requests to determine routing",
	}
	frontDeskExp, err := frontDeskBio.ToExpert(ctx, modelProvider, &toolwrap.Service{
		Auditor:       auditor,
		ApprovalStore: approvalStore,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create front desk expert: %w", err)
	}
	logger.GetLogger(ctx).Debug("Front desk expert created")

	// Create the summarizer for condensing tool outputs via the front desk model.
	summarizer, err := agentutils.NewSummarizer(ctx, modelProvider, auditor)
	if err != nil {
		return nil, fmt.Errorf("failed to create summarizer: %w", err)
	}
	logger.GetLogger(ctx).Debug("Summarizer created")

	// Initialize shared working memory for cross-turn observation sharing
	wm := rtmemory.NewWorkingMemory()

	// Initialize the ReAcTree executor with adaptive loop.
	// Instead of fixed stages (Understanding→Planning→Executing→Reviewing),
	// the LLM runs in a loop and dynamically decides how many iterations
	// it needs. Simple tasks finish in 1-2 iterations; complex ones can
	// use up to MaxIterations.
	// Use the persistent memory service for episodic memory as well.
	episodicMemoryCfg := rtmemory.DefaultEpisodicMemoryConfig()
	episodicMemoryCfg.Service = memorySvc
	treeExec := reactree.NewTreeExecutor(exp, wm, episodicMemoryCfg.NewEpisodicMemory(), reactree.TreeConfig{
		MaxDepth:            3,
		MaxDecisionsPerNode: 10,
		MaxTotalNodes:       30,
		MaxIterations:       10,
	})

	// Use provided memory.Service for conversation history persistence.
	logger.GetLogger(ctx).Info("Using persistent memory service")
	memoryUserKey := memory.UserKey{
		AppName: "genie-codeowner",
		UserID:  "default",
	}

	// Build the full tool set — these are available to sub-agents via create_agent,
	// but the codeowner itself only gets a restricted subset ( summarize, create_agent, hitl_readonly_tools).
	var allTools []tool.Tool
	allTools = append(allTools, tools...)

	// Initialize file tools scoped to the working directory.
	if ts, err := file.NewToolSet(file.WithBaseDir(workingDirectory)); err == nil {
		allTools = append(allTools, ts.Tools(ctx)...)
	}

	// Initialize local code executor for shell access (bash only for now)
	// This enables sub-agents to run verification commands like 'go test' or 'terraform validate'.
	exec := local.New(
		local.WithWorkDir(workingDirectory),
		local.WithTimeout(10*time.Minute),
		local.WithCleanTempFiles(true),
	)

	// Use ShellTool which wraps the code executor
	codeTool := NewShellTool(exec)
	allTools = append(allTools, codeTool)
	if vectorStore != nil {
		allTools = append(allTools, vector.NewMemoryStoreTool(vectorStore))
		allTools = append(allTools, vector.NewMemorySearchTool(vectorStore))
	}

	// Add the summarize_content tool so agents can invoke summarization on demand.
	summarizeTool := agentutils.NewSummarizerTool(summarizer)
	allTools = append(allTools, summarizeTool)

	// Create the create_agent tool for dynamic sub-agent spawning.
	// Sub-agents have access to the full tool set via the registry,
	// while the codeowner only uses list_file + summarize + create_agent.
	toolRegistry := make(reactree.ToolRegistry, len(allTools))
	for _, t := range allTools {
		toolRegistry[t.Declaration().Name] = t
	}
	toolWrapSvc := &toolwrap.Service{Auditor: auditor, ApprovalStore: approvalStore}
	logger.GetLogger(ctx).Info("codeowner: toolwrap.Service created for sub-agents",
		"hasAuditor", auditor != nil,
		"hasApprovalStore", approvalStore != nil,
	)
	createAgentTool := reactree.NewCreateAgentTool(
		modelProvider, summarizer, toolRegistry,
		wm,
		toolWrapSvc,
	)

	// Restrict the codeowner to orchestration-only tools:
	//   - list_file: understand project structure
	//   - summarize_content: condense large outputs
	//   - create_agent: delegate detailed work to sub-agents
	// All other tools (read_file, shell, etc.) are available to sub-agents only.
	codeOwnerTools := make(reactree.ToolRegistry)
	codeOwnerTools[createAgentTool.Declaration().Name] = createAgentTool
	for toolName, tool := range toolRegistry {
		if approvalStore.IsAllowed(toolName) {
			codeOwnerTools[toolName] = tool
		}
	}
	logger.GetLogger(ctx).Info("CodeOwner tool registry initialized", "count", len(codeOwnerTools))

	codeOwner := &codeOwner{
		expert:          exp,
		frontDeskExpert: frontDeskExp,
		workingMemory:   wm,
		treeExecutor:    treeExec,
		memorySvc:       memorySvc,
		memoryUserKey:   memoryUserKey,
		tools:           codeOwnerTools,
		auditor:         auditor,
	}
	codeOwner.resume, err = codeOwner.createResume(ctx, summarizer, toolRegistry, fullPersona)
	if err != nil {
		logger.GetLogger(ctx).Warn("failed to create the resume", "error", err)
	}
	return codeOwner, nil
}

// createResume generates a natural-language resume for the codeOwner agent
// based on the tools available to both the main agent and its subagents.
func (c *codeOwner) createResume(
	ctx context.Context,
	summarizer agentutils.Summarizer,
	registry reactree.ToolRegistry,
	fullPersona string,
) (string, error) {
	logger := logger.GetLogger(ctx)
	logger.Info("building my resume")
	// use the front desk expert to check on available tools and then create a resume
	result, err := summarizer.Summarize(ctx, agentutils.SummarizeRequest{
		RequiredOutputFormat: agentutils.OutputFormatMarkdown,
		Content: fmt.Sprintf(`Create a linkedIn worthy resume based and things that I can accomplish based on tools available to the given AI Agent:

- Talk in First Person
- You are trying to sell yourself to the user.
- Give some examples of what you can do.
- Keep it short and concise.

Persona:
%s

Available Skills:
%s`, fullPersona, registry.String()),
	})
	if err != nil {
		logger.Error("error creating resume", "error", err)
		return "", fmt.Errorf("error creating resume: %w", err)
	}
	return result, nil
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
	logr := logger.GetLogger(ctx).With("fn", "codeExpert.Chat")
	logr.Info("codeOwner.Chat invoked", "question", toolwrap.TruncateForAudit(req.Question, 100), "sender", req.SenderContext)

	// Step 1: Front desk classification — use a fast, cheap model to triage the request.
	category, err := c.classifyRequest(ctx, req.Question)
	if err != nil {
		// Classification failed — fall through to complex (fail-open)
		logr.Warn("front desk classification failed, defaulting to complex", "error", err)
		category = categoryComplex
	}
	logr.Info("front desk classified request", "category", category)

	// Audit: log classification result
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventClassification,
		Actor:     "front-desk",
		Action:    string(category),
		Metadata: map[string]interface{}{
			"question":       toolwrap.TruncateForAudit(req.Question, 200),
			"sender_context": req.SenderContext,
		},
	})

	switch category {
	case categoryRefuse:
		outputChan <- "I'm sorry, I can't help with that request."
		return nil

	case categorySalutation:
		// Use the main expert for a lightweight, tool-free salutation response.
		// We use the main expert (not frontDeskExpert) because frontDeskExpert
		// has the classifier prompt and would just output the category label.
		//
		// We wrap the user's message with an instruction to keep the response
		// conversational. The full persona prompt says "check the environment"
		// which, combined with MaxToolIterations=0, causes the model to
		// hallucinate file listings it never actually fetched.
		salutationMsg := fmt.Sprintf(
			"IMPORTANT: Respond conversationally to the greeting below. "+
				"Do NOT describe files, directories, or workspace contents. "+
				"Do NOT pretend you have inspected the environment. "+
				"Simply greet them and briefly mention what you can help with.\n\n%s",
			req.Question,
		)
		resp, err := c.expert.Do(ctx, expert.Request{
			Message:  salutationMsg,
			TaskType: modelprovider.TaskEfficiency,
			Mode: expert.ExpertConfig{
				MaxLLMCalls:       1,
				MaxToolIterations: 0,
			},
			EventChannel:  req.EventChan,
			SenderContext: req.SenderContext,
		})
		if err != nil {
			return fmt.Errorf("front desk salutation response failed: %w", err)
		}
		output := extractTextFromChoices(resp.Choices)
		outputChan <- output
		c.storeConversation(ctx, req.Question, output, req.SenderContext)
		return nil
	}

	// Determine task type based on classification
	taskType := modelprovider.TaskPlanning
	// For COMPLEX, we default to TaskPlanning, but if the user requested
	// specific tool types in other contexts, that logic would go here.

	// categoryComplex (default): Full ReAcTree pipeline

	// Retrieve relevant past conversation turns from memory.
	// Uses sender-based key so each sender/thread has isolated history.
	pastContext := c.recallConversation(ctx, req.Question, req.SenderContext)

	// Build the message with past conversation context injected
	message := req.Question
	if pastContext != "" {
		message = fmt.Sprintf(
			"## Relevant Past Conversations\n%s\n\n## Current Question\n%s",
			pastContext, req.Question,
		)
	}

	// Inject sender context so the LLM knows who is asking and from where.
	if req.SenderContext != "" {
		message = fmt.Sprintf("## Sender\n%s\n\n%s", req.SenderContext, message)
	}

	// Filter out excluded tools (e.g. send_message for messenger-originated messages).
	tools := c.tools.Exclude(req.ExcludeTools)

	runCtx := ctx
	if req.SenderContext != "" {
		// keys are in pkg/messenger/context.go
		// We use a raw string key approach if we don't want to import messenger package just for this,
		// but importing it is cleaner.
		// However, I need to make sure I import "github.com/appcd-dev/genie/pkg/messenger"
		runCtx = messenger.WithSenderContext(runCtx, req.SenderContext)
	}
	if req.BrowserTab != nil {
		// Use the browser tab context for chromedp operations, but respect
		// the parent context's cancellation and deadline. chromedp contexts
		// form their own hierarchy (rooted at the allocator), so we can't
		// make tabCtx a child of ctx directly. Instead we:
		// 1. Wrap the tab context in a cancellable context.
		// 2. Propagate parent's deadline to the tab context.
		// 3. Cancel the tab if the parent is cancelled.
		var tabCancel context.CancelFunc
		runCtx, tabCancel = context.WithCancel(req.BrowserTab)
		defer tabCancel()
		if deadline, ok := ctx.Deadline(); ok {
			var deadlineCancel context.CancelFunc
			runCtx, deadlineCancel = context.WithDeadline(runCtx, deadline)
			defer deadlineCancel()
		}
		// Ensure parent cancellation tears down the tab context too.
		go func() {
			select {
			case <-ctx.Done():
				tabCancel() // Parent cancelled — tear down the tab.
			case <-runCtx.Done():
			}
		}()
	}

	logr.Info("codeOwner: starting tree execution", "numTools", len(tools))

	// Execute using ReAcTree for structured reasoning and task decomposition
	res, err := c.treeExecutor.Run(runCtx, reactree.TreeRequest{
		Goal:          message,
		EventChan:     req.EventChan,
		Tools:         tools.Tools(),
		SenderContext: req.SenderContext,
		TaskType:      taskType,
	})

	if err != nil {
		return err
	}

	// Send the final result to the output channel
	outputChan <- res.Output

	// Store the conversation turn in memory for future recall (best-effort).
	c.storeConversation(ctx, req.Question, res.Output, req.SenderContext)

	// Audit: log complete conversation turn
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventConversation,
		Actor:     "code-owner",
		Action:    "chat_turn_completed",
		Metadata: map[string]interface{}{
			"question":       toolwrap.TruncateForAudit(req.Question, 200),
			"answer":         toolwrap.TruncateForAudit(res.Output, 500),
			"sender_context": req.SenderContext,
		},
	})

	return nil
}

// recallConversation searches memory.Service for past turns relevant
// to the current question and formats them as context.
// Uses senderID-based key so each sender/thread has isolated history.
func (c *codeOwner) recallConversation(ctx context.Context, question, senderID string) string {
	key := c.memoryKeyForSender(senderID)
	// Try to search for relevant memories
	entries, err := c.memorySvc.SearchMemories(ctx, key, question)
	if err != nil {
		logger.GetLogger(ctx).Warn("failed to search memories", "error", err)
	}

	// Fallback to recent history if search yields no results (common for "what did we just talk about?" queries)
	// or if the search implementation is basic (like our SQLite LIKE search).
	if len(entries) == 0 {
		recents, err := c.memorySvc.ReadMemories(ctx, key, 5)
		if err == nil {
			entries = recents
		}
	}

	if len(entries) == 0 {
		return ""
	}

	// Limit to top 3 most relevant past turns (or recent turns if fallback used)
	limit := 3
	if len(entries) < limit {
		limit = len(entries)
	}

	// Cap total recall size to prevent past conversations from
	// dominating the context window in multi-turn sessions.
	const maxRecallSize = 1500
	var sb strings.Builder
	for i := 0; i < limit; i++ {
		if entries[i] == nil || entries[i].Memory == nil {
			continue
		}
		entry := entries[i].Memory.Memory
		if sb.Len()+len(entry) > maxRecallSize {
			sb.WriteString("... (earlier turns omitted for brevity)\n")
			break
		}
		sb.WriteString(entry)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// storeConversation persists a Q&A turn into memory.Service.
// Uses senderID-based key so each sender/thread has isolated history.
func (c *codeOwner) storeConversation(ctx context.Context, question, answer, senderID string) {
	// Format the turn as a structured summary for retrieval.
	// Truncate both question and answer to prevent tool output from
	// prior turns from entering conversation memory and bloating
	// future context windows.
	summary := fmt.Sprintf("Q: %s\nA: %s",
		toolwrap.TruncateForAudit(question, 300),
		toolwrap.TruncateForAudit(answer, 500))
	topics := []string{"conversation", "chat-turn"}

	key := c.memoryKeyForSender(senderID)
	_ = c.memorySvc.AddMemory(ctx, key, summary, topics)
}

// memoryKeyForSender returns a memory.UserKey scoped to the given sender.
// When senderID is empty (e.g. TUI with no context), falls back to "default".
func (c *codeOwner) memoryKeyForSender(senderID string) memory.UserKey {
	if senderID == "" {
		return c.memoryUserKey // fallback to default
	}
	return memory.UserKey{
		AppName: c.memoryUserKey.AppName,
		UserID:  senderID,
	}
}

// filterTools returns a copy of tools with the named tools removed.
func filterTools(tools []tool.Tool, exclude []string) []tool.Tool {
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		excludeSet[name] = struct{}{}
	}
	filtered := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if _, skip := excludeSet[t.Declaration().Name]; !skip {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// loadAgentsGuide reads the Agents.md file from the given directory.
// If the file exists, its contents are returned so they can be appended
// to the agent's persona prompt, ensuring the agent honors project-level
// coding standards. Returns an empty string if the file does not exist
// or cannot be read (best-effort, non-fatal).
func loadAgentsGuide(dir string) string {
	if dir == "" {
		return ""
	}
	// case insensitive search for agents.mds
	agentsFile, err := osutils.FindFileCaseInsensitive(dir, "agents.md")
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(agentsFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// classifyRequest uses the front desk expert (a fast, lightweight model) to classify
// the incoming request into one of three categories: REFUSE, SALUTATION, or COMPLEX.
// On any error or unexpected response, defaults to categoryComplex (fail-open).
func (c *codeOwner) classifyRequest(ctx context.Context, question string) (requestCategory, error) {
	resp, err := c.frontDeskExpert.Do(ctx, expert.Request{
		Message:  question,
		TaskType: modelprovider.TaskEfficiency,
		Mode: expert.ExpertConfig{
			MaxLLMCalls:       1,
			MaxToolIterations: 0,
		},
	})
	if err != nil {
		return categoryComplex, fmt.Errorf("classification call failed: %w", err)
	}

	raw := strings.TrimSpace(extractTextFromChoices(resp.Choices))
	// The classifier should return a single word. Normalize to upper case
	// and check for known categories.
	normalized := strings.ToUpper(raw)

	switch {
	case strings.Contains(normalized, string(categoryRefuse)):
		return categoryRefuse, nil
	case strings.Contains(normalized, string(categorySalutation)):
		return categorySalutation, nil
	case strings.Contains(normalized, string(categoryComplex)):
		return categoryComplex, nil
	default:
		// Unexpected response — default to complex (fail-open)
		return categoryComplex, nil
	}
}

// extractTextFromChoices concatenates the text content from all model choices.
func extractTextFromChoices(choices []model.Choice) string {
	var sb strings.Builder
	for _, choice := range choices {
		if choice.Message.Content != "" {
			sb.WriteString(choice.Message.Content)
		}
	}
	return sb.String()
}
