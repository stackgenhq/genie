// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

// Package orchestrator implements Genie's request routing and execution flow:
// it receives user messages, classifies intent (simple vs complex), runs the
// appropriate expert(s), and coordinates ReAcTree multi-step workflows.
//
// It solves the problem of connecting the messenger layer to the agent layer:
// the orchestrator owns the conversation loop, injects skills/memory,
// applies HITL and clarification flows, and streams AG-UI events to the client.
// Without this package, there would be no single place that ties config, tools,
// experts, and execution together.
package orchestrator

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/agentutils"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/cron"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/halguard"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/llmutil"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
	"github.com/stackgenhq/genie/pkg/pii"
	"github.com/stackgenhq/genie/pkg/reactree"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/semanticrouter"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"github.com/stackgenhq/genie/pkg/ttlcache"
	"go.opentelemetry.io/otel/baggage"
	oteltrace "go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

//go:embed prompts/persona.txt
var persona string

type CodeQuestion struct {
	Question string `json:"question"`

	// SkipClassification when true skips front-desk classification and runs
	// the request as COMPLEX. Used for internal tasks (e.g. graph learn) that
	// would otherwise be misclassified as REFUSE.
	SkipClassification bool `json:"-"`

	// Attachments holds file/media attachments from the incoming message.
	// Image attachments are passed as multimodal content to the LLM so it
	// can "see" them; other types are described textually.
	Attachments []messenger.Attachment `json:"-"`

	// BrowserTab is an optional context for a specific browser tab.
	// If provided, browser tools will use this context.
	BrowserTab context.Context `json:"-"`
}

//go:generate go tool counterfeiter -generate

// Orchestrator is an expert that can answer users questions
//
//counterfeiter:generate . Orchestrator
type Orchestrator interface {
	Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error
	InjectFeedback(ctx context.Context, message string) error
	Close() error
	Resume(ctx context.Context) string
}

// requestCategory represents the front desk classification result.
type requestCategory string

const (
	categoryRefuse     requestCategory = "REFUSE"
	categorySalutation requestCategory = "SALUTATION"
	categoryOutOfScope requestCategory = "OUT_OF_SCOPE"
	categoryComplex    requestCategory = "COMPLEX"
)

type orchestrator struct {
	expert        expert.Expert
	treeExecutor  reactree.TreeExecutor
	memorySvc     memory.Service
	memoryUserKey memory.UserKey
	toolRegistry  *tools.Registry
	auditor       audit.Auditor
	resume        *ttlcache.Item[string]
	resumeCancel  context.CancelFunc
	vectorStore   vector.IStore
	router        semanticrouter.IRouter

	// availableToolNames holds the names of all tools registered in the full
	// tool registry (not just the orchestrator's own tools). This list is
	// injected into the resume so the front-desk classifier knows about
	// every capability (email, SCM, browser, etc.) when determining scope.
	availableToolNames []string

	// Per-sender memory isolation. These maps use sync.Map for concurrent
	// access and lazily create instances on first access per sender.
	workingMemories   sync.Map // map[string]*rtmemory.WorkingMemory
	episodicMemories  sync.Map // map[string]rtmemory.EpisodicMemory
	episodicMemoryCfg rtmemory.EpisodicMemoryConfig

	// importanceScorer assigns a 1-10 significance score to episodes.
	// Used by storeAccomplishment to score results before episodic storage.
	importanceScorer rtmemory.ImportanceScorer

	// wisdomStore provides access to consolidated wisdom notes.
	// Used by recallAccomplishments to surface distilled lessons.
	wisdomStore rtmemory.WisdomStore

	disableResume                     bool
	agentPersona                      string
	accomplishmentConfidenceThreshold float64
}

// Resume returns a natural language description of the agent's capabilities.
func (c *orchestrator) Resume(ctx context.Context) string {
	result, _ := c.resume.GetValue(ctx)
	return result
}

// NewOrchestrator creates a new orchestrator with an integrated ReAcTree executor.
// The working memory persists across chat turns, allowing the agent to share
// observations from previous interactions. The tree executor enables
// hierarchical task decomposition for complex queries when activated.
// The approvalStore enables HITL approval gating for sub-agent tool calls;
// when nil, sub-agents execute tools without requiring human approval.
// OrchestratorOption configures optional behaviour on the orchestrator.
type OrchestratorOption func(*orchestratorOpts)

type orchestratorOpts struct {
	toolwrapOpts                      []toolwrap.ServiceOption
	disableResume                     bool
	halGuardConfig                    halguard.Config
	semanticRouter                    semanticrouter.IRouter
	accomplishmentConfidenceThreshold float64
}

// WithToolwrapOptions passes per-agent middleware configuration to the
// underlying toolwrap.Service. Use this to enable rate limiting, tracing,
// retries, timeouts, etc. on a per-agent basis.
func WithToolwrapOptions(opts ...toolwrap.ServiceOption) OrchestratorOption {
	return func(o *orchestratorOpts) {
		o.toolwrapOpts = append(o.toolwrapOpts, opts...)
	}
}

// WithDisableResume sets whether to disable the dynamic generation of the agent's resume.
func WithDisableResume(disable bool) OrchestratorOption {
	return func(o *orchestratorOpts) {
		o.disableResume = disable
	}
}

// WithHalGuardConfig sets the hallucination guard configuration.
// When not provided, halguard.DefaultConfig() is used.
func WithHalGuardConfig(cfg halguard.Config) OrchestratorOption {
	return func(o *orchestratorOpts) {
		o.halGuardConfig = cfg
	}
}

// WithSemanticRouter sets the semantic router instance for fast
// embedding-based routing and caching.
func WithSemanticRouter(router semanticrouter.IRouter) OrchestratorOption {
	return func(o *orchestratorOpts) {
		o.semanticRouter = router
	}
}

// WithAccomplishmentConfidenceThreshold sets the minimum confidence score
// (0.0–1.0) required for a tree result to be stored as an accomplishment.
// Results below this threshold are discarded to prevent storing error
// outputs or low-quality responses as accomplishments. Default is 0.5.
func WithAccomplishmentConfidenceThreshold(threshold float64) OrchestratorOption {
	return func(o *orchestratorOpts) {
		o.accomplishmentConfidenceThreshold = threshold
	}
}

// agentNamePlaceholder is replaced in embedded prompt templates with the
// configured agent name so the LLM persona matches the deployment identity.
const agentNamePlaceholder = "{{AGENT_NAME}}"

func NewOrchestrator(
	ctx context.Context,
	modelProvider modelprovider.ModelProvider,
	availableTools *tools.Registry,
	vectorStore vector.IStore,
	auditor audit.Auditor,
	approvalStore hitl.ApprovalStore,
	memorySvc memory.Service,
	sessionSvc session.Service,
	agentPersona string,
	extraOpts ...OrchestratorOption,
) (Orchestrator, error) {
	var oo orchestratorOpts
	for _, fn := range extraOpts {
		fn(&oo)
	}
	// Resolve agent name from context (set by app.Bootstrap via
	// orchestratorcontext.WithAgent).
	agentName := orchestratorcontext.AgentFromContext(ctx).Name

	// Build the persona prompt. Replace the {{AGENT_NAME}} placeholder in
	// the embedded prompts with the configured agent name so the LLM
	// identifies itself correctly.
	fullPersona := strings.ReplaceAll(persona, agentNamePlaceholder, agentName)
	if agentPersona != "" {
		fullPersona += "\n\n" + agentPersona
	}

	expertBio := expert.ExpertBio{
		Personality: fullPersona,
		Name:        strings.ToLower(agentName),
		Description: agentName + " — strategic AI planner that decomposes requests into structured plans and orchestrates sub-agents",
	}

	var expertOpts []expert.ExpertOption
	if sessionSvc != nil {
		expertOpts = append(expertOpts, expert.WithExpertSessionService(sessionSvc))
	}
	// Create the summarizer for condensing tool outputs via the front desk model.
	summarizer, err := agentutils.NewSummarizer(ctx, modelProvider, auditor)
	if err != nil {
		return nil, fmt.Errorf("failed to create summarizer: %w", err)
	}
	logger.GetLogger(ctx).Debug("Summarizer created")

	// Tools are pre-built and pre-filtered by the tool registry (toolreg).
	// Build the reactree.ToolRegistry from the provided []tool.Tool.
	// Adapt the agentutils.Summarizer into a toolwrap.SummarizeFunc so the
	// middleware can auto-compress oversized tool results (>100 000 chars).
	summarizeFunc := toolwrap.SummarizeFunc(func(ctx context.Context, content string) (string, error) {
		return summarizer.Summarize(ctx, agentutils.SummarizeRequest{
			Content:              content,
			RequiredOutputFormat: agentutils.OutputFormatText,
		})
	})
	toolWrapSvc := toolwrap.NewService(auditor, approvalStore, summarizeFunc, oo.toolwrapOpts...)

	exp, err := expertBio.ToExpert(ctx, modelProvider, auditor, toolWrapSvc, expertOpts...)
	if err != nil {
		return nil, err
	}
	logger.GetLogger(ctx).Info("Expert bio created", "name", expertBio.Name, "persistentSession", sessionSvc != nil)

	// Initialize shared working memory for cross-turn observation sharing
	wm := rtmemory.NewWorkingMemory()

	// Keep the base episodic memory config for creating per-sender instances.
	episodicMemoryCfg := rtmemory.DefaultEpisodicMemoryConfig()
	episodicMemoryCfg.Service = memorySvc
	episodicMem := episodicMemoryCfg.NewEpisodicMemory()
	var checkpointer graph.CheckpointSaver
	if store, ok := sessionSvc.(interface{ Checkpointer() graph.CheckpointSaver }); ok {
		checkpointer = store.Checkpointer()
	}

	treeExec := reactree.NewTreeExecutor(exp, wm, episodicMem, reactree.TreeConfig{
		MaxDepth:            3,
		MaxDecisionsPerNode: 10,
		MaxTotalNodes:       30,
		MaxIterations:       3,
		Checkpointer:        checkpointer,
	})

	// Use provided memory.Service for conversation history persistence.
	logger.GetLogger(ctx).Info("Using persistent memory service")
	memoryUserKey := memory.UserKey{
		AppName: "genie:" + strings.ToLower(agentName),
		UserID:  "default",
	}

	logger.GetLogger(ctx).Info("orchestrator: toolwrap.Service created for sub-agents",
		"hasAuditor", auditor != nil,
		"hasApprovalStore", approvalStore != nil,
		"hasSummarizer", true,
	)
	// Create the hallucination guard for sub-agent output verification.
	// Uses the existing model provider to collect diverse models for
	// cross-model consistency checking per Finch-Zk methodology.
	// Config comes from [halguard] section in genie.toml; zero values
	// are filled with sensible defaults by halguard.New.
	//
	// The textGenerator callback routes halguard's LLM calls through
	// expert.Expert → trpc-agent-go runner → TraceChat, which gives
	// proper Langfuse tracing automatically (input, output, token usage).
	textGenerator := newHalguardTextGenerator(auditor, toolWrapSvc)
	halGuard := halguard.New(modelProvider, textGenerator,
		halguard.WithConfig(oo.halGuardConfig),
	)

	// Wire failure learning into sub-agents. Without this, sub-agent
	// failures are stored as raw error text without verbal reflections
	// or importance scores, limiting episodic memory effectiveness.
	failureReflector := reactree.NewExpertFailureReflector(exp)
	importanceScorer := reactree.NewExpertImportanceScorer(exp)

	// Wire pre-planning wisdom consultation. This creates a PlanAdvisor
	// that queries episodic memory and wisdom for relevant past experiences
	// BEFORE executing multi-step plans, enriching each step's context
	// with prior successes, failures, and consolidated lessons.
	wisdomStore := rtmemory.WisdomStoreConfig{
		Service: memorySvc,
		AppName: memoryUserKey.AppName,
		UserID:  memoryUserKey.UserID,
	}.NewWisdomStore()
	planAdvisor := rtmemory.NewPlanAdvisor(episodicMem, wisdomStore)

	createAgentTool := reactree.NewCreateAgentTool(
		modelProvider, exp, summarizer, availableTools,
		wm, episodicMem,
		toolWrapSvc,
		vectorStore,
		halGuard,
		reactree.WithSkipSummarizeMarker(true),
		reactree.WithFailureReflector(failureReflector),
		reactree.WithImportanceScorer(importanceScorer),
		reactree.WithPlanAdvisor(planAdvisor),
	)
	createAgentTool.SetHalGuardThreshold(oo.halGuardConfig.PreCheckThreshold)
	// Log tool counts so operators can verify email, gmail, etc. are wired for sub-agents.
	n := len(availableTools.ToolNames())
	logger := logger.GetLogger(ctx).With("fn", "createOrchestrator")

	logger.Info("create_agent sub-agent tools wired",
		"registry_total", n,
		"sub_agent_tools", n-2, // exclude create_agent and send_message
	)

	// Build the main agent's tool registry:
	//   - create_agent: to delegate detailed work to sub-agents
	//   - ask_clarifying_question: to ask users for clarification directly
	//   - note / read_notes: Pensieve context-management tools so the
	//     orchestrator can maintain persistent notes across its own turns
	//     (e.g. summarise sub-agent results before synthesising a reply).
	//
	// Sub-agent tool scoping (excluding create_agent + send_message) is
	// handled inside create_agent.go via subAgentRegistry, NOT here.
	orchestratorToolSlice := tools.Tools{
		createAgentTool.GetTool(),
	}
	// Lift ask_clarifying_question from the full registry so the main
	// agent (planner) can ask users directly without delegating to a sub-agent.
	if t, err := availableTools.GetTool("ask_clarifying_question"); err == nil {
		orchestratorToolSlice = append(orchestratorToolSlice, t)
	}
	// Lift create_recurring_task so the main agent can schedule recurring tasks
	// directly (e.g. "remind me daily", "run this every hour") without delegating.
	if t, err := availableTools.GetTool(cron.ToolName); err == nil {
		orchestratorToolSlice = append(orchestratorToolSlice, t)
	}
	// Lift Pensieve note tools and notify so the orchestrator can write/read persistent
	// notes across its own turns, distil sub-agent results into concise
	// summaries before composing a final answer, and send notifications.
	for _, toolName := range []string{"note", "read_notes", "delete_context", "check_budget", "notify", "memory_search", "memory_store", "memory_delete", "memory_list", "memory_merge"} {
		if t, err := availableTools.GetTool(toolName); err == nil {
			orchestratorToolSlice = append(orchestratorToolSlice, t)
		} else {
			logger.Debug("Failed to get tool", "tool_name", toolName, "error", err)
		}
	}
	orchestratorTools := tools.NewRegistry(ctx, orchestratorToolSlice)
	logger.Info("Orchestrator tool registry initialized", "count", len(orchestratorTools.ToolNames()))

	orchestrator := &orchestrator{
		expert:                            exp,
		treeExecutor:                      treeExec,
		memorySvc:                         memorySvc,
		memoryUserKey:                     memoryUserKey,
		toolRegistry:                      orchestratorTools,
		auditor:                           auditor,
		vectorStore:                       vectorStore,
		router:                            oo.semanticRouter,
		episodicMemoryCfg:                 episodicMemoryCfg,
		importanceScorer:                  importanceScorer,
		wisdomStore:                       wisdomStore,
		availableToolNames:                availableTools.GetToolDescriptions(),
		disableResume:                     oo.disableResume,
		agentPersona:                      agentPersona,
		accomplishmentConfidenceThreshold: oo.accomplishmentConfidenceThreshold,
	}
	// Apply default threshold if not explicitly set.
	if orchestrator.accomplishmentConfidenceThreshold == 0 {
		orchestrator.accomplishmentConfidenceThreshold = reactree.DefaultAccomplishmentConfidenceThreshold
	}
	// keep updating the resume less than 24 hours
	// Create a dedicated context for the background refresher that we can cancel on Close()
	resumeCtx, resumeCancel := context.WithCancel(context.Background())
	orchestrator.resumeCancel = resumeCancel

	orchestrator.resume = ttlcache.NewItem(func(ctx context.Context) (string, error) {
		return orchestrator.createResume(ctx, summarizer, fullPersona)
	}, 24*time.Hour)

	// Use WithoutCancel to detach from startup context, but we use our own resumeCtx
	// which we control via Close().
	go func() {
		if oo.disableResume {
			return
		}
		_, _ = orchestrator.resume.GetValue(resumeCtx)
		if err := orchestrator.resume.KeepItFresh(resumeCtx); err != nil && !errors.Is(err, context.Canceled) {
			// context.Canceled is expected on Close()
			logger.Error("failed to keep resume fresh", "err", err)
		}
	}()
	return orchestrator, nil
}

// createResume generates a natural-language resume for the orchestrator agent
// based on the tools available to both the main agent and its subagents,
// enriched with recent accomplishments from the vector memory store.
// Accomplishments act as confidence-building evidence that demonstrate
// the agent's track record to users. Without this, the resume would be
// purely theoretical ("I can do X") rather than evidence-based
// ("I have done X, Y, Z").
func (c *orchestrator) createResume(
	ctx context.Context,
	summarizer agentutils.Summarizer,
	fullPersona string,
) (string, error) {
	logger := logger.GetLogger(ctx)

	if c.disableResume {
		logger.Info("resume creation disabled, returning sanitized persona")

		// Avoid leaking the embedded orchestrator system prompt (persona.txt)
		// by stripping it from the combined persona before returning.
		sanitized := strings.TrimSpace(c.agentPersona)

		// If nothing remains after sanitization, fall back to a static,
		// non-sensitive message.
		if sanitized == "" {
			return "generalist", nil
		}

		return sanitized, nil
	}

	logger.Info("building my resume")

	// Inject a system-level MessageOrigin so downstream calls
	// (expert.getRunner → MessageOriginFrom) don't warn about missing origin.
	ctx = messenger.WithMessageOrigin(ctx, messenger.SystemMessageOrigin())

	// Retrieve recent accomplishments from vector memory to enrich the resume.
	accomplishments := c.recallAccomplishments(ctx)

	accomplishmentsSection := ""
	if accomplishments != "" {
		accomplishmentsSection = fmt.Sprintf(`

Recent Accomplishments (things I have successfully done):
%s`, accomplishments)
	}

	toolsSection := ""
	toolsInstruction := ""
	if len(c.availableToolNames) > 0 {
		sortedNames := make([]string, len(c.availableToolNames))
		copy(sortedNames, c.availableToolNames)
		sort.Strings(sortedNames)
		toolsSection = fmt.Sprintf(`

Available Tools (capabilities I can use via sub-agents):
%s`, strings.Join(sortedNames, ", "))
		toolsInstruction = "\n- The Available Tools section lists every tool the agent can delegate to sub-agents. Mention the key capability areas they enable (e.g. email, source control, browsing, file management)."
	}

	result, err := summarizer.Summarize(ctx, agentutils.SummarizeRequest{
		RequiredOutputFormat: agentutils.OutputFormatMarkdown,
		Content: fmt.Sprintf(`Create a resume based and things that I can accomplish based on tools available to the given AI Agent:

- Talk in First Person
- You are trying to sell yourself to the user.
- Give some examples of what you can do.
- Keep it short and concise.
- If accomplishments are available, highlight them as proof of your capabilities.%s

Persona:
%s
%s
%s`, toolsInstruction, fullPersona, accomplishmentsSection, toolsSection),
	})
	if err != nil {
		logger.Error("error creating resume", "error", err)
		return "", fmt.Errorf("error creating resume: %w", err)
	}
	return result, nil
}

// Close releases resources held by the orchestrator, including the conversation
// history memory service. Callers should defer Close() after NewOrchestrator.
func (c *orchestrator) Close() error {
	if c.resumeCancel != nil {
		c.resumeCancel()
	}
	if c.memorySvc != nil {
		return c.memorySvc.Close()
	}
	return nil
}

// bridgeBrowserTab creates a context that carries both the chromedp tab's
// executor and the per-request values from the caller's context (parent).
//
// chromedp contexts form their own hierarchy rooted at the allocator created
// at init time, so they never carry per-request values such as ThreadID,
// RunID, or MessageOrigin. This function bridges the two hierarchies by:
//  1. Deriving a cancellable context from the tab (for chromedp operations).
//  2. Inheriting the parent's deadline.
//  3. Re-injecting per-request values from the parent.
//  4. Propagating parent cancellation to the tab.
//
// Without this, any tool call running inside a browser-tab context would
// silently lose AG-UI identifiers, breaking HITL approval and sub-agent
// correlation.
//
// The returned CancelFunc must be deferred by the caller.
func bridgeBrowserTab(parent, tab context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(tab)

	if deadline, ok := parent.Deadline(); ok {
		var dlCancel context.CancelFunc
		ctx, dlCancel = context.WithDeadline(ctx, deadline)
		baseCancel := cancel
		cancel = func() { dlCancel(); baseCancel() }
	}

	if origin := messenger.MessageOriginFrom(parent); !origin.IsZero() {
		ctx = messenger.WithMessageOrigin(ctx, origin)
	}
	if tid := agui.ThreadIDFromContext(parent); tid != "" {
		ctx = agui.WithThreadID(ctx, tid)
	}
	if rid := agui.RunIDFromContext(parent); rid != "" {
		ctx = agui.WithRunID(ctx, rid)
	}

	// Propagate OTel tracing context so child spans maintain the
	// same traceID and parent-child hierarchy. Without this, spans
	// created inside the browser-tab context become orphaned root
	// spans and appear as separate traces in Langfuse.
	if parentSpan := oteltrace.SpanFromContext(parent); parentSpan.SpanContext().IsValid() {
		ctx = oteltrace.ContextWithSpan(ctx, parentSpan)
	}

	// Propagate OTel baggage so Langfuse attributes (userId,
	// sessionId, tags, metadata) are inherited by downstream spans.
	if parentBag := baggage.FromContext(parent); parentBag.Len() > 0 {
		ctx = baggage.ContextWithBaggage(ctx, parentBag)
	}

	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx, cancel
}

// classifyAndMaybeShortCircuit runs front-desk classification (unless skipped) and
// handles REFUSE, OUT_OF_SCOPE, and SALUTATION by sending a response and returning
// (true, nil). For COMPLEX it returns (false, nil) so the caller continues with the
// full pipeline. Returns (_, err) on salutation response failure.
func (c *orchestrator) classifyAndMaybeShortCircuit(ctx context.Context, req CodeQuestion, outputChan chan<- string) (handled bool, err error) {
	category, cr := c.classifyRequest(ctx, req)

	logr := logger.GetLogger(ctx).With("fn", "classifyAndMaybeShortCircuit")
	logr.Info("classification result", "category", category, "reason", cr.Reason, "bypassed_llm", cr.BypassedLLM)

	// emitShortCircuit sends the response to both the outputChan (for non-AGUI
	// platforms like Slack/Discord) and the AG-UI event bus (for web UI clients).
	agentName := orchestratorcontext.AgentFromContext(ctx).Name
	emitShortCircuit := func(msg string) {
		logr.Info("emitShortCircuit", "msg_len", len(msg), "msg_preview", toolwrap.TruncateForAudit(msg, 100))
		agui.EmitAgentMessage(ctx, agentName, msg)
		outputChan <- msg
	}

	return c.handleShortCircuit(ctx, category, cr, req, emitShortCircuit)
}

func (c *orchestrator) classifyRequest(ctx context.Context, req CodeQuestion) (requestCategory, semanticrouter.ClassificationResult) {
	logr := logger.GetLogger(ctx).With("fn", "classifyRequest")
	if req.SkipClassification {
		logr.Info("front desk classification skipped (internal task)", "category", categoryComplex)
		return categoryComplex, semanticrouter.ClassificationResult{}
	}

	cr, classifyErr := c.router.Classify(ctx, req.Question, c.Resume(ctx))
	if classifyErr != nil {
		logr.Warn("semantic gatekeeper classification failed, defaulting to complex", "error", classifyErr)
		cr = semanticrouter.ClassificationResult{Category: semanticrouter.CategoryComplex}
	}

	var category requestCategory
	switch cr.Category {
	case semanticrouter.CategoryRefuse:
		category = categoryRefuse
	case semanticrouter.CategorySalutation:
		category = categorySalutation
	case semanticrouter.CategoryOutOfScope:
		category = categoryOutOfScope
	default:
		category = categoryComplex
	}

	if !cr.BypassedLLM {
		logr.Info("front desk classified request via LLM", "category", category)
		c.auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventClassification,
			Actor:     "front-desk",
			Action:    string(category),
			Metadata:  map[string]interface{}{"question": req.Question},
		})
	} else {
		logr.Info("front desk classified request via L1 Semantic Router", "category", category)
	}

	return category, cr
}

func (c *orchestrator) handleShortCircuit(ctx context.Context, category requestCategory, cr semanticrouter.ClassificationResult, req CodeQuestion, emit func(string)) (bool, error) {
	switch category {
	case categoryRefuse:
		emit("🚫 Whoa there! That's a no-go zone for me. " +
			"I'm here to build cool things, not blow them up! " +
			"Got something constructive? I'm all ears 👂")
		return true, nil
	case categoryOutOfScope:
		reason := cr.Reason
		if reason == "" {
			reason = "That doesn't seem to be within my area of expertise."
		}
		emit(fmt.Sprintf(
			"🙈 Hmm, I can't help with that — %s\n\n"+
				"Try me with something in my wheelhouse — I promise I'm great at it! 🚀",
			reason,
		))
		return true, nil
	case categorySalutation:
		salutationMsg := fmt.Sprintf(
			"IMPORTANT: Respond conversationally to the greeting below. "+
				"Do NOT describe files, directories, or workspace contents. "+
				"Do NOT pretend you have inspected the environment. "+
				"Simply greet them and briefly mention what you can help with.\n\n%s",
			req.Question,
		)
		logr := logger.GetLogger(ctx).With("fn", "handleShortCircuit.salutation")
		resp, doErr := c.expert.Do(ctx, expert.Request{
			Message:  salutationMsg,
			TaskType: modelprovider.TaskEfficiency,
			Mode: expert.ExpertConfig{
				MaxLLMCalls:       1,
				MaxToolIterations: 0,
			},
		})
		if doErr != nil {
			return true, fmt.Errorf("front desk salutation response failed: %w", doErr)
		}
		logr.Info("Expert.Do salutation response",
			"choices_count", len(resp.Choices),
			"has_usage", resp.Usage != nil,
		)
		for i, ch := range resp.Choices {
			logr.Info("salutation choice",
				"index", i,
				"content_len", len(ch.Message.Content),
				"content_preview", toolwrap.TruncateForAudit(ch.Message.Content, 200),
				"role", ch.Message.Role,
				"finish_reason", ch.FinishReason,
				"tool_calls_count", len(ch.Message.ToolCalls),
			)
		}
		output := llmutil.ExtractChoiceContent(resp.Choices)
		logr.Info("salutation output", "output_len", len(output), "output_preview", toolwrap.TruncateForAudit(output, 200))
		emit(output)
		c.storeConversation(ctx, req.Question, output)
		return true, nil
	default:
		return false, nil
	}
}

func (c *orchestrator) Chat(ctx context.Context, req CodeQuestion, outputChan chan<- string) error {
	// Create a child span so all sub-operations (recall, tree execution,
	// store, audit) are grouped under the parent trace.
	// Use agentName+".chat" (not the bare agent name) to avoid colliding
	// with langfuse.trace.name — a bare-name match causes the Langfuse
	// exporter to promote this span to a separate top-level trace.
	ctx, chatSpan := trace.Tracer.Start(ctx, orchestratorcontext.AgentNameFromContext(ctx)+".chat")
	defer chatSpan.End()

	logr := logger.GetLogger(ctx).With("fn", "codeExpert.Chat")
	logr.Info("codeOwner.Chat invoked", "question", toolwrap.TruncateForAudit(req.Question, 100))

	// Semantic router check for cache hit.
	// Internal tasks (cron triggers, heartbeats) skip the cache entirely:
	// cron actions must always execute fresh, and their results should not
	// pollute the cache for future user queries.
	if c.router != nil && !req.SkipClassification {
		if cachedResult, hit := c.router.CheckCache(ctx, req.Question); hit {
			logr.Info("semantic cache hit", "question", toolwrap.TruncateForAudit(req.Question, 50))
			// Emit via AG-UI event bus so streaming clients (web UI) see the
			// response.  The outputChan consumer in buildChatHandler skips
			// emitting for AG-UI (to avoid duplicates with the tree executor),
			// but the cache-hit path never enters the tree, so we must emit
			// explicitly — matching the pattern used by emitShortCircuit.
			agui.EmitAgentMessage(ctx, orchestratorcontext.AgentNameFromContext(ctx), cachedResult)
			outputChan <- cachedResult
			return nil
		}
	}

	handled, err := c.classifyAndMaybeShortCircuit(ctx, req, outputChan)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	// Use TaskPlanning for complex requests — the planner persona needs
	// a reasoning-heavy model to analyze, decompose, and plan before
	// delegating execution to sub-agents via create_agent.
	taskType := modelprovider.TaskPlanning

	// categoryComplex (default): Full ReAcTree pipeline

	// Retrieve relevant past conversation turns from memory.
	// Uses sender-based key so each sender/thread has isolated history.
	pastContext := c.recallConversation(ctx, req.Question)

	// Retrieve relevant past episodes from episodic memory.
	// Episodes capture prior Q&A turns and their outcomes, enabling
	// the agent to learn from its own history.
	episodicContext := c.recallEpisodes(ctx, req.Question)

	// Build the message with past conversation context injected.
	message := req.Question
	if pastContext != "" || episodicContext != "" {
		var contextParts []string
		if pastContext != "" {
			contextParts = append(contextParts, fmt.Sprintf("## Relevant Past Conversations\n%s", pastContext))
		}
		if episodicContext != "" {
			contextParts = append(contextParts, fmt.Sprintf("## Relevant Past Episodes\n%s", episodicContext))
		}
		message = fmt.Sprintf(
			"%s\n\n## Current Question\n%s",
			strings.Join(contextParts, "\n\n"), req.Question,
		)
	}

	runCtx := ctx
	if req.BrowserTab != nil {
		var cleanup context.CancelFunc
		runCtx, cleanup = bridgeBrowserTab(ctx, req.BrowserTab)
		defer cleanup()
	}

	// Stash the original question in context so toolwrap can persist it in
	// the approval row — needed for replay-on-resume after server restart.
	runCtx = toolwrap.WithOriginalQuestion(runCtx, req.Question)

	// Execute using ReAcTree for structured reasoning and task decomposition
	res, err := c.treeExecutor.Run(runCtx, reactree.TreeRequest{
		Goal:           message,
		Tools:          c.toolRegistry.GetTools(),
		ToolGetter:     c.toolRegistry.GetTools,
		TaskType:       taskType,
		Attachments:    req.Attachments,
		WorkingMemory:  c.workingMemoryForSender(ctx),
		EpisodicMemory: c.episodicMemoryForSender(ctx),
	})

	if err != nil {
		return err
	}

	// Send the final result to the output channel
	outputChan <- res.Output

	// Store the conversation turn in memory for future recall (best-effort).
	c.storeConversation(ctx, req.Question, res.Output)

	// Store the Q&A turn as an episode in episodic memory so future turns
	// can recall what was asked and how it was answered. Stored as "pending"
	// until a user reaction (👍/👎) upgrades or downgrades it.
	c.storeEpisode(ctx, req.Question, res)

	// Persist the result to semantic cache.
	// Skip for internal tasks so cron/heartbeat results don't pollute
	// the cache — they'd cause false cache hits for unrelated user queries
	// that happen to be semantically similar.
	if c.router != nil && !req.SkipClassification {
		if err := c.router.SetCache(ctx, req.Question, res.Output); err != nil {
			logr.Warn("failed to set semantic cache", "error", err)
		}
	}

	// Persist a concise accomplishment so the agent's resume can reference
	// real work it has completed, boosting user confidence.
	c.storeAccomplishment(ctx, req.Question, res)

	// Audit: log complete conversation turn
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventConversation,
		Actor:     "orchestrator",
		Action:    "chat_turn_completed",
		Metadata: map[string]interface{}{
			"question": req.Question,
			"answer":   res.Output,
		},
	})

	return nil
}

// InjectFeedback injects a user message into the working memory of the current sender
// so the agent can naturally adapt to "mid-stream" instructions during an ongoing ReAcTree execution.
func (c *orchestrator) InjectFeedback(ctx context.Context, message string) error {
	wm := c.workingMemoryForSender(ctx)

	// Format as a clear directive
	feedback := fmt.Sprintf("INTERRUPT: New User Instruction Received: %s", message)

	// Add a Unix nanosecond timestamp to ensure the key is unique even for rapid injections
	key := fmt.Sprintf("user_feedback_%d", time.Now().UnixNano())

	wm.Store(key, feedback)

	logger.GetLogger(ctx).Info("injected user feedback into working memory", "key", key)
	return nil
}

// recallConversation searches memory.Service for past turns relevant
// to the current question and formats them as context.
// Uses senderID-based key so each sender/thread has isolated history.
func (c *orchestrator) recallConversation(ctx context.Context, question string) string {
	convKey := c.conversationKeyFromContext(ctx)
	key := c.memoryKeyForSender(convKey)
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

	// Audit: log memory recall.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "orchestrator",
		Action:    "recall_conversation",
		Metadata: map[string]interface{}{
			"results": limit,
		},
	})

	return sb.String()
}

// storeConversation persists a Q&A turn into memory.Service.
// Uses senderID-based key so each sender/thread has isolated history.
// PII is redacted before storage to prevent sensitive data leakage.
func (c *orchestrator) storeConversation(ctx context.Context, question, answer string) {
	// Format the turn as a structured summary for retrieval.
	// Truncate both question and answer to prevent tool output from
	// prior turns from entering conversation memory and bloating
	// future context windows.
	summary := fmt.Sprintf("Q: %s\nA: %s",
		toolwrap.TruncateForAudit(question, 300),
		toolwrap.TruncateForAudit(answer, 500))

	// Redact PII before persisting to prevent sensitive data leakage.
	summary = pii.Redact(summary)

	topics := []string{"conversation", "chat-turn"}

	convKey := c.conversationKeyFromContext(ctx)
	key := c.memoryKeyForSender(convKey)
	_ = c.memorySvc.AddMemory(ctx, key, summary, topics)

	// Audit: log memory write.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "orchestrator",
		Action:    "store_conversation",
		Metadata:  map[string]interface{}{},
	})
}

// recallEpisodes retrieves relevant past Q&A episodes from episodic memory
// and formats them as context for the current question. This gives the
// orchestrator the ability to learn from its own interaction history.
// Without this, the agent would have no awareness of how it previously
// handled similar questions, leading to repeated mistakes or redundant work.
func (c *orchestrator) recallEpisodes(ctx context.Context, question string) string {
	episodic := c.episodicMemoryForSender(ctx)

	// Retrieve up to 3 most relevant past episodes.
	episodes := episodic.Retrieve(ctx, question, 3)
	if len(episodes) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, ep := range episodes {
		sb.WriteString(ep.String())
		sb.WriteString("\n\n")
	}

	// Audit: log episodic memory recall.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "orchestrator",
		Action:    "recall_episodes",
		Metadata: map[string]interface{}{
			"results": len(episodes),
		},
	})

	return sb.String()
}

// storeEpisode persists a Q&A turn as an episode in episodic memory.
// The episode is stored with "pending" status so that user reactions
// (👍 → success, 👎 → failure) can later validate or invalidate it.
// This prevents memory poisoning from graceful LLM failures — only
// human-validated episodes are promoted to "success" status.
// PII is redacted before storage to prevent sensitive data leakage.
func (c *orchestrator) storeEpisode(ctx context.Context, question string, res reactree.TreeResult) {
	// Only store episodes that produced meaningful output.
	if strings.TrimSpace(res.Output) == "" {
		return
	}

	episodic := c.episodicMemoryForSender(ctx)

	// Cap trajectory to prevent large outputs from bloating future prompts.
	trajectory := pii.Redact(toolwrap.TruncateForAudit(res.Output, 500))
	goal := pii.Redact(toolwrap.TruncateForAudit(question, 300))

	// Map tree status to episode status. Successful completions are
	// stored as pending (awaiting human validation); failures are
	// stored directly as failures.
	epStatus := rtmemory.EpisodePending
	if res.Status != reactree.Success {
		epStatus = rtmemory.EpisodeFailure
	}

	episodic.Store(ctx, rtmemory.Episode{
		Goal:       goal,
		Trajectory: trajectory,
		Status:     epStatus,
	})

	logger.GetLogger(ctx).Info("orchestrator Q&A stored in episodic memory",
		"goal", toolwrap.TruncateForAudit(question, 60),
		"status", epStatus,
	)

	// Audit: log episodic memory write.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "orchestrator",
		Action:    "store_episode",
		Metadata: map[string]interface{}{
			"goal_length":       len(goal),
			"trajectory_length": len(trajectory),
			"status":            string(epStatus),
		},
	})
}

// recallAccomplishments searches the vector store for recent accomplishments
// and formats them as a bulleted list for inclusion in the agent's resume.
// recallAccomplishments retrieves past accomplishments from the episodic memory
// pipeline (primary) and falls back to the legacy vector store when episodic
// memory has no entries yet.
//
// Primary source: weighted episodic episodes (importance-scored, recency-decayed)
// combined with consolidated wisdom notes from the WisdomStore.
//
// Fallback: legacy vector store entries (type=accomplishment) for backward
// compatibility with entries stored before the episodic pipeline migration.
func (c *orchestrator) recallAccomplishments(ctx context.Context) string {
	var sb strings.Builder

	// 1. Primary: episodic memory (weighted by recency × importance).
	episodic := c.episodicMemoryForSender(ctx)
	episodes := episodic.RetrieveWeighted(ctx, "accomplishment", 5)
	for _, ep := range episodes {
		sb.WriteString("- ")
		sb.WriteString(ep.Trajectory)
		sb.WriteString("\n")
	}

	// 2. Append consolidated wisdom notes (distilled daily lessons).
	if c.wisdomStore != nil {
		notes := c.wisdomStore.RetrieveWisdom(ctx, 3)
		for _, note := range notes {
			sb.WriteString("- ")
			sb.WriteString(note.Summary)
			sb.WriteString("\n")
		}
	}

	// 3. Fallback: legacy vector store entries for backward compatibility.
	// Once all entries have been migrated to episodic memory, this can be removed.
	if sb.Len() == 0 && c.vectorStore != nil {
		filter := map[string]string{
			"type": rtmemory.AccomplishmentType,
		}
		origin := messenger.MessageOriginFrom(ctx)
		filter["visibility"] = origin.DeriveVisibility()

		results, err := c.vectorStore.SearchWithFilter(ctx, rtmemory.AccomplishmentType, 50, filter)
		if err != nil {
			logger.GetLogger(ctx).Warn("failed to search legacy accomplishments", "error", err)
			return ""
		}

		// Fallback: if private context yields sparse results, also search by
		// sender_id across all visibility scopes.
		const minResults = 2
		if origin.IsPrivateContext() && len(results) < minResults {
			senderFilter := map[string]string{
				"type":      rtmemory.AccomplishmentType,
				"sender_id": origin.Sender.ID,
			}
			extraResults, err := c.vectorStore.SearchWithFilter(ctx, rtmemory.AccomplishmentType, 50, senderFilter)
			if err == nil {
				seen := make(map[string]bool, len(results))
				for _, r := range results {
					seen[r.ID] = true
				}
				for _, r := range extraResults {
					if !seen[r.ID] {
						results = append(results, r)
					}
				}
			}
		}

		if len(results) == 0 {
			return ""
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		limit := 5
		if len(results) < limit {
			limit = len(results)
		}

		for i := 0; i < limit; i++ {
			sb.WriteString("- ")
			sb.WriteString(results[i].Content)
			sb.WriteString("\n")
		}
	}

	if sb.Len() == 0 {
		return ""
	}

	// Audit: log memory recall.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "orchestrator",
		Action:    "recall_accomplishments",
		Metadata: map[string]interface{}{
			"episodic_count": len(episodes),
		},
	})

	return sb.String()
}

// storeAccomplishment routes a completed task through the episodic memory
// pipeline instead of writing raw Q&A directly to the vector store.
//
// The episodic pipeline provides:
//   - ImportanceScorer: LLM-scored 1-10 significance (routine lookups → 2-3,
//     novel lessons → 8-9) prevents trivial or ephemeral results from
//     inflating the memory corpus.
//   - Recency decay: older episodes naturally lose weight (λ=0.01,
//     ~3% after 14 days) so time-sensitive data fades automatically.
//   - EpisodeConsolidator: daily cron batches raw episodes into wisdom
//     notes, distilling 20 raw entries into 1-2 concise lessons.
//
// PII is redacted before storage to prevent sensitive data leakage.
func (c *orchestrator) storeAccomplishment(ctx context.Context, question string, res reactree.TreeResult) {
	logr := logger.GetLogger(ctx).With("fn", "storeAccomplishment")

	// Only store if the tree completed successfully with non-empty output
	// and sufficient confidence.
	if res.Status != reactree.Success || strings.TrimSpace(res.Output) == "" {
		return
	}
	if res.Confidence < c.accomplishmentConfidenceThreshold {
		logr.Debug("skipping accomplishment storage: confidence below threshold",
			"confidence", res.Confidence,
			"threshold", c.accomplishmentConfidenceThreshold,
		)
		return
	}

	// Build a concise trajectory from the Q&A turn.
	goalText := toolwrap.TruncateForAudit(question, 200)
	trajectory := fmt.Sprintf("Q: %s\nA: %s",
		goalText,
		toolwrap.TruncateForAudit(res.Output, 500))

	// Redact PII before persisting to prevent sensitive data leakage.
	trajectory = pii.Redact(trajectory)

	// Score importance using the LLM-backed scorer. Routine lookups
	// (e.g. "what's the AWS cost?") score 2-3; novel achievements
	// (e.g. "refactored the auth module") score 8-9.
	importance := c.importanceScorer.Score(ctx, rtmemory.ImportanceScoringRequest{
		Goal:   goalText,
		Output: trajectory,
		Status: rtmemory.EpisodeSuccess,
	})

	// Store as an episode in the per-sender episodic memory.
	// This feeds into the existing consolidation pipeline:
	//   EpisodicMemory → ImportanceScorer → EpisodeConsolidator → WisdomStore
	episodic := c.episodicMemoryForSender(ctx)
	episodic.Store(ctx, rtmemory.Episode{
		Goal:       goalText,
		Trajectory: trajectory,
		Status:     rtmemory.EpisodeSuccess,
		Importance: importance,
	})

	logr.Info("accomplishment stored as episode",
		"importance", importance,
		"goal_length", len(goalText),
		"trajectory_length", len(trajectory),
	)

	// Audit: log memory write.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "orchestrator",
		Action:    "store_accomplishment",
		Metadata: map[string]interface{}{
			"trajectory_length": len(trajectory),
			"importance":        importance,
		},
	})
}

// memoryKeyForSender returns a memory.UserKey scoped to the given sender.
// When senderID is empty (e.g. TUI with no context), falls back to "default".
func (c *orchestrator) memoryKeyForSender(senderID string) memory.UserKey {
	if senderID == "" {
		return c.memoryUserKey // fallback to default
	}
	return memory.UserKey{
		AppName: c.memoryUserKey.AppName,
		UserID:  senderID,
	}
}

// workingMemoryForSender returns (or creates) an isolated WorkingMemory
// instance for the given sender. This prevents User A's observations from
// leaking into User B's context.
func (c *orchestrator) workingMemoryForSender(ctx context.Context) *rtmemory.WorkingMemory {
	sender := c.conversationKeyFromContext(ctx)
	if sender == "global" || sender == "" {
		sender = "default"
	}
	if existing, ok := c.workingMemories.Load(sender); ok {
		return existing.(*rtmemory.WorkingMemory)
	}
	newWM := rtmemory.NewWorkingMemory()
	actual, _ := c.workingMemories.LoadOrStore(sender, newWM)
	return actual.(*rtmemory.WorkingMemory)
}

// episodicMemoryForSender returns (or creates) an isolated EpisodicMemory
// instance for the given sender. Each sender has its own episode pool.
func (c *orchestrator) episodicMemoryForSender(ctx context.Context) rtmemory.EpisodicMemory {
	origin := messenger.MessageOriginFrom(ctx)
	sender := origin.DeriveVisibility()
	if sender == "" {
		sender = "default"
	}
	if existing, ok := c.episodicMemories.Load(sender); ok {
		return existing.(rtmemory.EpisodicMemory)
	}
	newEp := c.episodicMemoryCfg.WithUserID(sender).NewEpisodicMemory()
	actual, _ := c.episodicMemories.LoadOrStore(sender, newEp)
	return actual.(rtmemory.EpisodicMemory)
}

// conversationKeyFromContext returns the appropriate memory key for
// conversation history isolation. In group chats, all members share
// history (keyed by channel ID). In DMs/AG-UI, each thread has its
// own history (keyed by sender + channel/thread ID).
//
// Uses DeriveConversationKey (not DeriveVisibility) because conversation
// recall needs per-thread isolation: each AG-UI chat session creates a
// fresh threadId, and we must not leak prior threads' context into new ones.
//
// When running under GUILD with multiple agents, the key is prefixed
// with the agent name so agents sharing a channel have isolated memory.
func (c *orchestrator) conversationKeyFromContext(ctx context.Context) string {
	origin := messenger.MessageOriginFrom(ctx)
	key := origin.DeriveConversationKey()
	if agentName := audit.AgentNameFromContext(ctx); agentName != "" {
		key = agentName + ":" + key
	}
	return key
}

// ForgetUser clears all memory stores associated with a given sender.
// This implements the "right to be forgotten" for privacy compliance.
func (c *orchestrator) ForgetUser(ctx context.Context, senderID string) error {
	// 1. Clear conversation history.
	key := c.memoryKeyForSender(senderID)
	if err := c.memorySvc.ClearMemories(ctx, key); err != nil {
		return fmt.Errorf("failed to clear conversation memory: %w", err)
	}

	// 2. Clear working memory for this sender.
	if wm, ok := c.workingMemories.LoadAndDelete(senderID); ok {
		wm.(*rtmemory.WorkingMemory).Clear()
	}

	// 3. Clear episodic memory for this sender.
	// Episodic memory is backed by memory.Service with a scoped UserKey,
	// so clearing its memory.Service entries removes episodes.
	episodicKey := memory.UserKey{
		AppName: "reactree",
		UserID:  senderID,
	}
	_ = c.memorySvc.ClearMemories(ctx, episodicKey)
	c.episodicMemories.Delete(senderID)

	// 4. Delete vector store accomplishments for this sender.
	// Vector store entries are tagged with sender_id metadata, so we
	// search and delete matching entries.
	if c.vectorStore != nil {
		results, err := c.vectorStore.SearchWithFilter(ctx, rtmemory.AccomplishmentType, 100, map[string]string{
			"sender_id": senderID,
		})
		if err == nil {
			ids := make([]string, 0, len(results))
			for _, r := range results {
				ids = append(ids, r.ID)
			}
			if len(ids) > 0 {
				_ = c.vectorStore.Delete(ctx, ids...)
			}
		}
	}

	// 5. Audit the forget operation.
	c.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventMemoryAccess,
		Actor:     "system",
		Action:    "forget_user",
		Metadata: map[string]interface{}{
			"sender_id": senderID,
		},
	})

	return nil
}
