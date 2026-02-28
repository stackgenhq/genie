package expert

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
	messengeragui "github.com/stackgenhq/genie/pkg/messenger/agui"
	"github.com/stackgenhq/genie/pkg/osutils"
	rtmemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/retrier"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

type ExpertBio struct {
	Name        string
	Description string
	Personality string
	Tools       []tool.Tool
}

// ExpertOption configures optional expert behavior.
type ExpertOption func(*expert)

// WithExpertSessionService injects a session.Service for persistent
// conversation history. When set, the expert uses this service instead
// of creating a new inmemory.SessionService per runner.
func WithExpertSessionService(svc session.Service) ExpertOption {
	return func(e *expert) { e.sessionSvc = svc }
}

func (e ExpertBio) ToExpert(
	ctx context.Context,
	modelProvider modelprovider.ModelProvider,
	auditor audit.Auditor,
	toolwrapSvc *toolwrap.Service,
	expertOpts ...ExpertOption,
) (_ Expert, err error) {
	exp := &expert{
		bio:           e,
		modelProvider: modelProvider,
		eventAdapter:  messengeragui.NewEventAdapter(e.Name),
		auditor:       auditor,
		toolwrapSvc:   toolwrapSvc,
	}
	for _, o := range expertOpts {
		o(exp)
	}
	return exp, nil
}

type Request struct {
	Message         string
	AdditionalTools []tool.Tool
	// Process each choices as they are generated
	ChoiceProcessor func(choices ...model.Choice) `json:"-"`
	// TaskType to use
	TaskType modelprovider.TaskType

	Mode ExpertConfig

	// WorkingMemory is an optional shared memory used to cache file-read tool results.
	// When set, ToolWrapper will automatically cache results from read_file, list_file,
	// and read_multiple_files, preventing redundant reads within the same session.
	WorkingMemory *rtmemory.WorkingMemory

	// Attachments holds file/media attachments from the incoming message.
	// Image attachments with LocalPath are added as multimodal content parts
	// so the LLM can "see" them. Other attachments are described textually.
	Attachments []messenger.Attachment
}

func (r Request) mode() []llmagent.Option {
	defaultMode := []llmagent.Option{
		llmagent.WithGenerationConfig(model.GenerationConfig{
			Temperature: model.Float64Ptr(0.3),
			Stream:      true,
		}),
		llmagent.WithEnableParallelTools(true),                           // Enable parallel tool execution for better performance
		llmagent.WithAddCurrentTime(true),                                // Automatically inject current time into prompts
		llmagent.WithTimeFormat("Monday, January 02, 2006 15:04:05 MST"), // Format time to be explicit about the date
		// Token optimization: Prevent runaway costs with iteration limits
		llmagent.WithMaxLLMCalls(25),
		llmagent.WithMaxToolIterations(20),
		// Token optimization: Only include current request context, not full history (50-70% savings)
		llmagent.WithMessageFilterMode(llmagent.RequestContext),
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
	defaultMode = append(defaultMode, llmagent.WithMaxToolIterations(r.Mode.MaxToolIterations))
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
	eventAdapter  *messengeragui.EventAdapter
	// Auditor is an audit logger for recording LLM calls and tool invocations.
	auditor audit.Auditor

	// toolwrapSvc holds the session-stable dependencies for tool wrapping
	// (auditor, approval store). Per-request fields (EventChan, WorkingMemory)
	// are passed via toolwrap.WrapRequest at runner creation time.
	toolwrapSvc *toolwrap.Service

	// sessionSvc is an optional injected session service for persistent
	// conversation history. When nil, a new inmemory service is created.
	sessionSvc session.Service

	// runner is persisted across Do() calls so the in-memory session service
	// retains conversation history for multi-turn chat.
	runner       runner.Runner
	sessionID    string
	lastTaskType modelprovider.TaskType
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
	logr.Debug("model selected for expert", "providers", modelInstance.Providers())

	// Combine tools and wrap them using the toolwrap service.
	allTools := append(e.bio.Tools, req.AdditionalTools...)
	origin := messenger.MessageOriginFrom(ctx)
	logr.Info("wrapping tools with MessageOrigin",
		"hasOrigin", !origin.IsZero(),
		"origin", fmt.Sprintf("%v", origin),
	)
	wrappedTools := e.toolwrapSvc.Wrap(allTools, toolwrap.WrapRequest{
		WorkingMemory: req.WorkingMemory,
		AgentName:     e.bio.Name,
	})

	// Debug: Log tool definitions being sent
	logr.Info("tools prepared for expert",
		"bio_tools_count", len(e.bio.Tools),
		"additional_tools_count", len(req.AdditionalTools),
		"total_tools", len(wrappedTools),
	)

	opts := append(req.mode(),
		llmagent.WithTools(wrappedTools),
		llmagent.WithModels(modelInstance),
		llmagent.WithDescription(e.bio.Description),
		llmagent.WithInstruction(e.bio.Personality),
		// PII protection: redact user messages before sending to the LLM,
		// rehydrate originals in the response so the user sees unmasked output.
		llmagent.WithModelCallbacks(NewPIIModelCallbacks()),
	)

	theExpert := llmagent.New(
		e.bio.Name,
		opts...,
	)
	// Use injected session service if available (DB-backed for persistence);
	// otherwise fall back to inmemory.
	sessionSvc := e.sessionSvc
	if sessionSvc == nil {
		sessionSvc = inmemory.NewSessionService(
			inmemory.WithSummarizer(summary.NewSummarizer(
				modelInstance.GetAny(),
				summary.WithTokenThreshold(4000),
				summary.WithName("expert-summarizer"),
			)),
		)
	}
	return runner.NewRunner(e.bio.Name, theExpert,
		runner.WithSessionService(sessionSvc),
	), nil
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
	if e.runner == nil || taskType != e.lastTaskType {
		r, err := e.getRunner(ctx, req)
		if err != nil {
			return Response{}, fmt.Errorf("could not create a runner for the expert: %w", err)
		}
		e.runner = r
		e.lastTaskType = taskType
		e.sessionID = uuid.NewString() // stable session ID for the lifetime of this expert
	}

	startTime := time.Now()
	defer func() {
		logr.Info("Expert.Do", "duration", time.Since(startTime).String())
	}()
	logr.Debug("expert is working on the request", "msg_preview", toolwrap.TruncateForAudit(req.Message, 120))

	// Audit: log LLM request
	e.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventLLMRequest,
		Actor:     e.bio.Name,
		Action:    "llm_call_started",
		Metadata: map[string]interface{}{
			"task_type":  string(taskType),
			"session_id": e.sessionID,
			"message":    req.Message,
		},
	})

	sessionID := e.sessionID
	if sessionID == "" {
		sessionID = messenger.MessageOriginFrom(ctx).String()
	}

	// Retry transient upstream LLM errors (503 / rate-limit / overloaded).
	var evCh <-chan *event.Event
	err := retrier.Retry(ctx, func() error {
		var runErr error
		evCh, runErr = e.runner.Run(ctx,
			osutils.Getenv("USER", "anonymous"),
			sessionID,
			buildUserMessage(ctx, req),
		)
		return runErr
	},
		retrier.WithAttempts(3),
		retrier.WithBackoffDuration(5*time.Second),
		retrier.WithRetryIf(IsTransientError),
		retrier.WithOnRetry(func(attempt int, err error) {
			logr.Warn("transient LLM error, will retry",
				"attempt", attempt, "error", err.Error())
		}),
	)
	if err != nil {
		return HandleExpertError(ctx, err)
	}
	response := Response{}
	for ev := range evCh {
		if req.ChoiceProcessor != nil {
			req.ChoiceProcessor(ev.Choices...)
		}
		response.Choices = append(response.Choices, ev.Choices...)
		e.emitEventToTUI(ctx, ev)

		// Debug: log agent thought preview (short to avoid log spam)
		for _, choice := range ev.Choices {
			if choice.Message.Content != "" {
				logr.Debug("agent thought", "content", toolwrap.TruncateForAudit(choice.Message.Content, 80))
			}
		}
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
func (e *expert) emitEventToTUI(ctx context.Context, event *event.Event) {
	if e.eventAdapter == nil {
		return
	}

	// Use the event adapter to convert trpc-agent-go events to TUI messages
	messages := e.eventAdapter.ConvertEvent(event)
	for _, msg := range messages {
		agui.Emit(ctx, msg)
	}
}

// buildUserMessage constructs a model.Message from a Request. When the request
// contains attachments with a local file path (e.g., WhatsApp downloads):
//   - Images are embedded as visual ContentParts via AddImageFilePath (vision).
//   - Other files (PDFs, documents) are embedded via AddFilePath (document input).
//
// Attachments without a LocalPath are already described textually in req.Message.
func buildUserMessage(ctx context.Context, req Request) model.Message {
	msg := model.NewUserMessage(req.Message)

	for _, att := range req.Attachments {
		if att.LocalPath == "" {
			continue // No local file to embed — URL/text description already in message.
		}
		if isImageMIME(att.ContentType) {
			// Embed image as visual content so the LLM can "see" it.
			if err := msg.AddImageFilePath(att.LocalPath, "auto"); err != nil {
				logger.GetLogger(ctx).Warn(
					"failed to add image attachment to user message",
					"path", att.LocalPath,
					"error", err,
				)
			}
		} else {
			// Embed PDF / document / other file as file content.
			if err := msg.AddFilePath(att.LocalPath); err != nil {
				logger.GetLogger(ctx).Warn(
					"failed to add file attachment to user message",
					"path", att.LocalPath,
					"error", err,
				)
			}
		}
	}

	return msg
}

// isImageMIME returns true if the content type is an image.
func isImageMIME(contentType string) bool {
	return len(contentType) > 6 && contentType[:6] == "image/"
}
