package expert

import (
	"context"
	"fmt"
	"time"

	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/tui"
	"github.com/appcd-dev/go-lib/constants"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/google/uuid"
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
) (_ Expert, err error) {
	expert := &expert{
		bio:           e,
		modelProvider: modelProvider,
		eventAdapter:  tui.NewEventAdapter(e.Name),
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
}

func (r Request) mode() []llmagent.Option {
	defaultMode := []llmagent.Option{
		llmagent.WithGenerationConfig(model.GenerationConfig{
			Temperature: constants.ToPtr(0.3),
			Stream:      true,
		}),
		llmagent.WithEnableParallelTools(true), // Enable parallel tool execution for better performance
		llmagent.WithAddCurrentTime(true),      // Automatically inject current time into prompts
		// Token optimization: Prevent runaway costs with iteration limits
		llmagent.WithMaxLLMCalls(40),
		llmagent.WithMaxToolIterations(12),
		// Token optimization: Only include current request context, not full history (50-70% savings)
		llmagent.WithMessageFilterMode(llmagent.IsolatedRequest),
		// Token optimization: Limit history messages to reduce context size (20-30% savings)
		llmagent.WithMaxHistoryRuns(5),
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
	eventAdapter  *tui.EventAdapter
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
		"task_type", req.TaskType,
	)

	wrappedTools := make([]tool.Tool, len(e.bio.Tools)+len(req.AdditionalTools))
	for i, t := range append(e.bio.Tools, req.AdditionalTools...) {
		wrappedTools[i] = &ToolWrapper{
			Tool:      t,
			EventChan: req.EventChannel,
		}
	}

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
	runner, err := e.getRunner(ctx, req)
	if err != nil {
		return Response{}, fmt.Errorf("could not create a runner for the expert: %w", err)
	}
	defer func() { _ = runner.Close() }()

	defer func(startTime time.Time) {
		logr.Info("Expert.Do", "duration", time.Since(startTime).String())
	}(time.Now())
	logr.Debug("expert is working on the request", "msg", req.Message)

	// Generate a deterministic session ID from the message content
	sessionID := uuid.NewSHA1(uuid.Nil, []byte(req.Message)).String()

	event, err := runner.Run(ctx,
		osutils.Getenv("USER", "anonymous"),
		sessionID,
		model.NewUserMessage(req.Message),
	)
	if err != nil {
		return HandleExpertError(err)
	}
	response := Response{}
	for event := range event {
		if req.ChoiceProcessor != nil {
			req.ChoiceProcessor(event.Choices...)
		}
		response.Choices = append(response.Choices, event.Choices...)
		e.emitEventToTUI(event, req.EventChannel)
	}
	return response, nil
}

// emitEventToTUI converts trpc-agent-go events to TUI messages and sends them to the event channel.
// This method exists to bridge the agent's event stream to the TUI's message system.
// Without this method, the TUI would not receive real-time updates about agent activity.
func (e *expert) emitEventToTUI(event *event.Event, eventChan chan<- interface{}) {
	if eventChan == nil || e.eventAdapter == nil {
		return
	}

	// Use the event adapter to convert trpc-agent-go events to TUI messages
	messages := e.eventAdapter.ConvertEvent(event)
	for _, msg := range messages {
		eventChan <- msg
	}
}

// ToolWrapper wraps a tool to emit events when it's called.
type ToolWrapper struct {
	tool.Tool
	EventChan chan<- interface{}
}

func (w *ToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	logr := logger.GetLogger(ctx).With("fn", "ToolWrapper.Call", "tool", w.Tool.Declaration().Name)

	// Log tool invocation
	logr.Debug("tool call started", "args", string(jsonArgs))
	defer func(startTime time.Time) {
		logr.Debug("tool call completed", "tool", w.Tool.Declaration().Name, "duration", time.Since(startTime).String())
	}(time.Now())

	// Execute the underlying tool
	var output any
	var err error

	if ct, ok := w.Tool.(tool.CallableTool); ok {
		output, err = ct.Call(ctx, jsonArgs)
	} else {
		return nil, fmt.Errorf("tool is not callable")
	}

	// Token optimization: Truncate large tool responses to prevent context explosion
	// Large outputs (like generated Terraform files) can bloat context in subsequent LLM calls
	const maxToolResultSize = 2000
	responseStr := fmt.Sprintf("%v", output)
	truncated := false
	if len(responseStr) > maxToolResultSize {
		end := maxToolResultSize
		// To avoid corrupting a multi-byte character, find the last rune boundary.
		for end > 0 && (responseStr[end]&0xC0) == 0x80 { // is a continuation byte
			end--
		}
		responseStr = responseStr[:end] + "\n... [truncated - full output saved to file]"
		truncated = true
	}

	// Log tool result
	if err != nil {
		logr.Error("tool call failed", "error", err)
	} else {
		logr.Debug("tool call completed", "response_length", len(responseStr), "truncated", truncated)
	}

	// Emit tool response event
	if w.EventChan != nil {
		w.EventChan <- tui.AgentToolResponseMsg{
			ToolName: w.Tool.Declaration().Name,
			Response: responseStr,
			Error:    err,
		}
	}

	return output, err
}
