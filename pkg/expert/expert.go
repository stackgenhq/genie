package expert

import (
	"context"
	"fmt"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	"github.com/appcd-dev/go-lib/constants"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type ExpertBio struct {
	Name        string
	Description string
	Personality string
	Tools       []tool.Tool
}

func (e ExpertBio) ToExpert(
	ctx context.Context,
	modelProvider modelprovider.ModelProvider,
	auditor audit.Auditor,
	toolwrapSvc ...*toolwrap.Service,
) (_ Expert, err error) {
	var svc *toolwrap.Service
	if len(toolwrapSvc) > 0 && toolwrapSvc[0] != nil {
		svc = toolwrapSvc[0]
	} else {
		// Default: create a service with just the auditor (backward compatible).
		svc = &toolwrap.Service{Auditor: auditor}
	}
	expert := &expert{
		bio:           e,
		modelProvider: modelProvider,
		eventAdapter:  agui.NewEventAdapter(e.Name),
		auditor:       auditor,
		toolwrapSvc:   svc,
	}
	return expert, nil
}

type Request struct {
	Message         string
	AdditionalTools []tool.Tool
	// EventChannel is an optional channel for emitting TUI events.
	// If provided, the expert will emit events for thinking, streaming, tool calls, etc.
	EventChannel chan<- interface{}
	// Process each choices as they are generated
	ChoiceProcessor func(choices ...model.Choice) `json:"-"`
	// TaskType to use
	TaskType modelprovider.TaskType

	Mode ExpertConfig

	// WorkingMemory is an optional shared memory used to cache file-read tool results.
	// When set, ToolWrapper will automatically cache results from read_file, list_file,
	// and read_multiple_files, preventing redundant reads within the same session.
	WorkingMemory *rtmemory.WorkingMemory

	// SenderContext identifies the user/channel for this request.
	// Used to isolate session history for multi-user environments.
	SenderContext string
}

func (r Request) mode() []llmagent.Option {
	defaultMode := []llmagent.Option{
		llmagent.WithGenerationConfig(model.GenerationConfig{
			Temperature: constants.ToPtr(0.3),
			Stream:      true,
		}),
		llmagent.WithEnableParallelTools(true),                           // Enable parallel tool execution for better performance
		llmagent.WithAddCurrentTime(true),                                // Automatically inject current time into prompts
		llmagent.WithTimeFormat("Monday, January 02, 2006 15:04:05 MST"), // Format time to be explicit about the date
		// Token optimization: Prevent runaway costs with iteration limits
		llmagent.WithMaxLLMCalls(25),
		llmagent.WithMaxToolIterations(20),
		// Token optimization: Only include current request context, not full history (50-70% savings)
		llmagent.WithMessageFilterMode(llmagent.IsolatedRequest),
		// Token optimization: Limit history messages to reduce context size (20-30% savings)
		llmagent.WithMaxHistoryRuns(3),
		// Token optimization: Discard reasoning chains from previous turns (10-20% savings)
		llmagent.WithReasoningContentMode(llmagent.ReasoningContentModeDiscardPreviousTurns),
	}
	if r.Mode.DisableParallelTools {
		defaultMode = append(defaultMode, llmagent.WithEnableParallelTools(false))
	}
	if r.Mode.MaxLLMCalls > 0 {
		defaultMode = append(defaultMode, llmagent.WithMaxLLMCalls(r.Mode.MaxLLMCalls))
	}
	if r.Mode.MaxToolIterations > 0 {
		defaultMode = append(defaultMode, llmagent.WithMaxToolIterations(r.Mode.MaxToolIterations))
	}
	if r.Mode.MaxHistoryRuns > 0 {
		defaultMode = append(defaultMode, llmagent.WithMaxHistoryRuns(r.Mode.MaxHistoryRuns))
	}
	if r.Mode.ReasoningContentMode != "" {
		defaultMode = append(defaultMode, llmagent.WithReasoningContentMode(r.Mode.ReasoningContentMode))
	}
	return defaultMode
}

type Response struct {
	Choices []model.Choice
}

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . Expert
type Expert interface {
	Do(ctx context.Context, req Request) (Response, error)
}

type expert struct {
	bio           ExpertBio
	modelProvider modelprovider.ModelProvider
	eventAdapter  *agui.EventAdapter
	// Auditor is an optional audit logger for recording LLM calls and tool invocations.
	// When nil, no audit events are emitted.
	auditor audit.Auditor

	// toolwrapSvc holds the session-stable dependencies for tool wrapping
	// (auditor, approval store). Per-request fields (EventChan, WorkingMemory)
	// are passed via toolwrap.WrapRequest at runner creation time.
	toolwrapSvc *toolwrap.Service

	// runner is persisted across Do() calls so the in-memory session service
	// retains conversation history for multi-turn chat.
	runner       runner.Runner
	sessionID    string
	lastTaskType modelprovider.TaskType

	// lastEventChan tracks the EventChannel used when the runner was last
	// created. When a new request arrives with a different EventChannel
	// (e.g. a new HTTP request), the runner must be recreated so that
	// Wrapper instances reference the correct, open channel.
	// Without this, stale Wrappers from a previous request panic
	// with "send on closed channel".
	lastEventChan chan<- interface{}
}

func (e *expert) getRunner(ctx context.Context, req Request) (runner.Runner, error) {
	logr := logger.GetLogger(ctx).With("fn", "expert.getRunner", "agent", e.bio.Name)
	if req.TaskType == "" {
		req.TaskType = modelprovider.TaskPlanning
	}

	modelInstance, err := e.modelProvider.GetModel(ctx, req.TaskType)
	if err != nil {
		return nil, fmt.Errorf("could not get a model to perform planning: %w", err)
	}

	// Debug: Log model information
	logr.Debug("model selected for expert",
		"model_name", modelInstance.Info().Name,
	)

	// Combine tools and wrap them using the toolwrap service.
	allTools := append(e.bio.Tools, req.AdditionalTools...)
	wrappedTools := e.toolwrapSvc.Wrap(allTools, toolwrap.WrapRequest{
		EventChan:     req.EventChannel,
		WorkingMemory: req.WorkingMemory,
	})

	// Debug: Log tool definitions being sent
	logr.Info("tools prepared for expert",
		"bio_tools_count", len(e.bio.Tools),
		"additional_tools_count", len(req.AdditionalTools),
		"total_tools", len(wrappedTools),
	)
	for i, t := range wrappedTools {
		decl := t.Declaration()
		schemaType := "nil"
		if decl.InputSchema != nil {
			schemaType = decl.InputSchema.Type
		}
		logr.Info("tool declaration",
			"index", i,
			"name", decl.Name,
			"description_len", len(decl.Description),
			"has_input_schema", decl.InputSchema != nil,
			"schema_type", schemaType,
		)
	}

	opts := append(req.mode(),
		llmagent.WithTools(wrappedTools),
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription(e.bio.Description),
		llmagent.WithInstruction(e.bio.Personality),
	)

	theExpert := llmagent.New(
		e.bio.Name,
		opts...,
	)
	return runner.NewRunner(e.bio.Name, theExpert), nil
}

func (e *expert) Do(ctx context.Context, req Request) (Response, error) {
	logr := logger.GetLogger(ctx).With("fn", "Expert.Do", "agent", e.bio.Name)

	// Lazily create the runner on first call and reuse it for subsequent calls.
	// This keeps the in-memory session service alive, preserving chat history.
	// When TaskType changes (e.g., between stages), recreate the runner with the
	// new model. A new session is required because different providers can't
	// share the same history context.
	taskType := req.TaskType
	if taskType == "" {
		taskType = modelprovider.TaskPlanning
	}
	// Recreate the runner when the task type changes OR when the event
	// channel changes. The latter is critical: toolwrap.Wrapper instances
	// capture a reference to EventChannel at construction time. If the
	// runner is reused across HTTP requests, the embedded Wrappers
	// still point to the previous request's (now-closed) event channel,
	// causing "send on closed channel" panics.
	eventChanChanged := req.EventChannel != nil && req.EventChannel != e.lastEventChan
	if e.runner == nil || taskType != e.lastTaskType || eventChanChanged {
		r, err := e.getRunner(ctx, req)
		if err != nil {
			return Response{}, fmt.Errorf("could not create a runner for the expert: %w", err)
		}
		e.runner = r
		e.lastTaskType = taskType
		e.lastEventChan = req.EventChannel
		e.sessionID = uuid.NewString() // stable session ID for the lifetime of this expert
	}

	startTime := time.Now()
	defer func() {
		logr.Info("Expert.Do", "duration", time.Since(startTime).String())
	}()
	logr.Debug("expert is working on the request", "msg", req.Message)

	// Audit: log LLM request
	e.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventLLMRequest,
		Actor:     e.bio.Name,
		Action:    "llm_call_started",
		Metadata: map[string]interface{}{
			"task_type":  string(taskType),
			"session_id": e.sessionID,
			"message":    toolwrap.TruncateForAudit(req.Message, 500),
		},
	})

	sessionID := e.sessionID
	if req.SenderContext != "" {
		sessionID = req.SenderContext
	}

	event, err := e.runner.Run(ctx,
		osutils.Getenv("USER", "anonymous"),
		sessionID,
		model.NewUserMessage(req.Message),
		// Detached cancel is intentional: the runner's session service
		// retains conversation history across HTTP requests. If the HTTP
		// request context cancelled the runner, it would tear down the
		// session prematurely, losing multi-turn chat history. The runner
		// is reused (and re-cancelled) via expert.Do's lifecycle instead.
		agent.WithDetachedCancel(true),
	)
	if err != nil {
		return HandleExpertError(ctx, err)
	}
	response := Response{}
	for event := range event {
		if req.ChoiceProcessor != nil {
			req.ChoiceProcessor(event.Choices...)
		}
		response.Choices = append(response.Choices, event.Choices...)
		e.emitEventToTUI(ctx, event, req.EventChannel)
	}

	// Audit: log LLM response
	e.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventLLMResponse,
		Actor:     e.bio.Name,
		Action:    "llm_call_completed",
		Metadata: map[string]interface{}{
			"task_type":    string(taskType),
			"session_id":   e.sessionID,
			"choice_count": len(response.Choices),
			"duration_ms":  time.Since(startTime).Milliseconds(),
		},
	})

	return response, nil
}

// emitEventToTUI converts trpc-agent-go events to TUI messages and sends them to the event channel.
// This method exists to bridge the agent's event stream to the TUI's message system.
// Without this method, the TUI would not receive real-time updates about agent activity.
func (e *expert) emitEventToTUI(ctx context.Context, event *event.Event, eventChan chan<- interface{}) {
	if eventChan == nil || e.eventAdapter == nil {
		return
	}

	// Recover from panics when sending on a potentially closed channel.
	defer func() {
		if r := recover(); r != nil {
			logger.GetLogger(ctx).Warn("recovered panic in emitEventToTUI", "panic", r)
		}
	}()

	// Use the event adapter to convert trpc-agent-go events to TUI messages
	messages := e.eventAdapter.ConvertEvent(event)
	for _, msg := range messages {
		eventChan <- msg
	}
}
