package agui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/appcd-dev/go-lib/logger"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// EventAdapter converts trpc-agent-go events to TUI messages.
// This adapter exists to bridge the gap between the agent's event stream and the TUI's message system.
// Without this adapter, the TUI would not be able to understand and display agent events.
type EventAdapter struct {
	agentName        string
	currentMessageID string // tracks open TEXT_MESSAGE for lifecycle
}

// NewEventAdapter creates a new event adapter for the given agent name.
func NewEventAdapter(agentName string) *EventAdapter {
	return &EventAdapter{
		agentName: agentName,
	}
}

// FlushMessage emits a TEXT_MESSAGE_END for any open message.
// Call this when the event stream ends to close the lifecycle.
func (a *EventAdapter) FlushMessage() []interface{} {
	if a.currentMessageID == "" {
		return nil
	}
	msgID := a.currentMessageID
	a.currentMessageID = ""
	return []interface{}{
		TextMessageEndMsg{Type: EventTextMessageEnd, MessageID: msgID},
	}
}

// ConvertEvent converts a trpc-agent-go event to one or more TUI messages.
// This method exists to translate agent events into TUI-specific message types.
// Without this conversion, the TUI would receive raw events that it doesn't know how to handle.
//
// This method handles both standard trpc-agent-go events and custom event types used by the granter workflow.
func (a *EventAdapter) ConvertEvent(evt *event.Event) []interface{} {
	if evt == nil || evt.Response == nil {
		return nil
	}

	var messages []interface{}

	// Check for API-level errors in the response first
	if evt.Error != nil {
		// Special handling for max tool iterations to provide a user-friendly prompt
		if strings.Contains(evt.Error.Message, "max tool iterations") {
			return []interface{}{
				AgentChatMessage{
					Type:    EventTextMessageContent,
					Sender:  "Genie",
					Message: "I have run into my limits (max tool iterations). Do you want me to keep trying? (Reply 'yes' to continue)",
				},
			}
		}

		errMsg := buildErrorMessage(evt.Error)
		messages = append(messages, AgentErrorMsg{
			Type:    EventRunError,
			Error:   fmt.Errorf("%s", errMsg),
			Context: evt.Error.Type,
		})
		return messages
	}

	// Process each choice in the event
	for _, choice := range evt.Choices {
		// Handle tool responses (result of a tool execution)
		// These events have ToolID set on the message, indicating which tool call they respond to.
		// Must be checked before content handling to avoid emitting tool output as chat text.
		if choice.Message.ToolID != "" {
			messages = append(messages, AgentToolResponseMsg{
				Type:       EventToolCallResult,
				ToolCallID: choice.Message.ToolID,
				Response:   choice.Message.Content,
			})
			continue // Don't also emit this as a stream chunk
		}

		// Handle reasoning content
		if choice.Message.ReasoningContent != "" {
			messages = append(messages, AgentReasoningMsg{
				Type:    EventReasoningMessageContent,
				Content: choice.Message.ReasoningContent,
				Delta:   true, // Streaming events are always deltas
			})
		}

		// Handle streaming content (skip for tool call events which have their own rendering)
		if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
			// Emit TEXT_MESSAGE_START on the first chunk of a new message.
			if a.currentMessageID == "" {
				a.currentMessageID = uuid.NewString()
				messages = append(messages, TextMessageStartMsg{
					Type:      EventTextMessageStart,
					MessageID: a.currentMessageID,
				})
			}
			messages = append(messages, AgentStreamChunkMsg{
				Type:      EventTextMessageContent,
				MessageID: a.currentMessageID,
				Content:   choice.Message.Content,
				Delta:     true, // Streaming events are always deltas
			})
		}

		// Handle tool calls (LLM requesting a tool invocation)
		if len(choice.Message.ToolCalls) > 0 {
			// Close any open text message before tool calls.
			if a.currentMessageID != "" {
				messages = append(messages, TextMessageEndMsg{
					Type:      EventTextMessageEnd,
					MessageID: a.currentMessageID,
				})
				a.currentMessageID = ""
			}
			for _, toolCall := range choice.Message.ToolCalls {
				messages = append(messages, AgentToolCallMsg{
					Type:       EventToolCallStart,
					ToolName:   toolCall.Function.Name,
					Arguments:  string(toolCall.Function.Arguments),
					ToolCallID: toolCall.ID,
				})
				// Emit TOOL_CALL_ARGS with the full argument payload.
				messages = append(messages, ToolCallArgsMsg{
					Type:       EventToolCallArgs,
					ToolCallID: toolCall.ID,
					Delta:      string(toolCall.Function.Arguments),
				})
				// Emit TOOL_CALL_END since the LLM has finished specifying this call.
				messages = append(messages, ToolCallEndMsg{
					Type:       EventToolCallEnd,
					ToolCallID: toolCall.ID,
				})
			}
		}

		// Handle finish reason (it's a pointer to string)
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finishReason := *choice.FinishReason
			switch finishReason {
			case "stop":
				// Normal completion — close any open text message.
				if a.currentMessageID != "" {
					messages = append(messages, TextMessageEndMsg{
						Type:      EventTextMessageEnd,
						MessageID: a.currentMessageID,
					})
					a.currentMessageID = ""
				}
			case "error":
				// Agent finished with error - extract details from message content
				errContext := "during execution"
				errMsg := "agent finished with error"
				if choice.Message.Content != "" {
					errMsg = choice.Message.Content
				}
				messages = append(messages, AgentErrorMsg{
					Type:    EventRunError,
					Error:   fmt.Errorf("%s", errMsg),
					Context: errContext,
				})
			case "content_filter":
				messages = append(messages, AgentErrorMsg{
					Type:    EventRunError,
					Error:   fmt.Errorf("response was blocked by content safety filter"),
					Context: "content_filter",
				})
			case "length":
				messages = append(messages, AgentErrorMsg{
					Type:    EventRunError,
					Error:   fmt.Errorf("response was truncated due to token limit"),
					Context: "length_exceeded",
				})
			}
		}
	}

	return messages
}

// buildErrorMessage constructs a detailed error message from ResponseError.
func buildErrorMessage(e *model.ResponseError) string {
	msg := e.Message
	if e.Type != "" {
		msg = fmt.Sprintf("[%s] %s", e.Type, msg)
	}
	if e.Code != nil && *e.Code != "" {
		msg = fmt.Sprintf("%s (code: %s)", msg, *e.Code)
	}
	return msg
}

// Start starts the event adapter.
// It listens to the input channel for events and sends converted TUI messages to the output channel.
// This method blocks until the input channel is closed.
func (a *EventAdapter) Start(
	ctx context.Context,
	inputChan <-chan interface{},
	outputChan chan<- interface{},
) {
	logger := logger.GetLogger(ctx).With("fn", "agui.EventAdapter.Start")
	defer close(outputChan)
	// Ensure any open text message is closed at the end of the stream.
	defer func() {
		for _, msg := range a.FlushMessage() {
			select {
			case outputChan <- msg:
			case <-time.After(100 * time.Millisecond):
				// Don't block forever on flush if output is full/stalled
			}
		}
	}()

	for rawEvent := range inputChan {
		// Convert event.Event to TUI messages
		if evt, ok := rawEvent.(*event.Event); ok {
			messages := a.ConvertEvent(evt)
			for _, msg := range messages {
				// Use blocking send to ensure no messages are dropped
				outputChan <- msg
			}
		} else {
			// Pass through other event types as-is.
			if _, isUserInput := rawEvent.(UserInputMsg); isUserInput {
				logger.Info("adapter: forwarding UserInputMsg to TUI")
				outputChan <- rawEvent
			} else {
				// Use blocking send to ensure no events are dropped
				outputChan <- rawEvent
			}
		}
	}
}
