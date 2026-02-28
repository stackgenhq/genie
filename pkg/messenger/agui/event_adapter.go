package agui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// repetitionWindowSize is the length of the substring we check for repeated patterns.
// When the same phrase of this length appears repetitionThreshold times at the end
// of accumulated content, we truncate to stop runaway LLM output.
const repetitionWindowSize = 100

// repetitionThreshold is how many consecutive repeats trigger truncation.
const repetitionThreshold = 4

// maxAccumulatedContentLen caps the buffer size used for repetition detection.
const maxAccumulatedContentLen = 50000

// EventAdapter converts trpc-agent-go events to TUI messages.
// This adapter exists to bridge the gap between the agent's event stream and the TUI's message system.
// Without this adapter, the TUI would not be able to understand and display agent events.
type EventAdapter struct {
	agentName            string
	currentMessageID     string // tracks open TEXT_MESSAGE for lifecycle
	accumulatedContent   string // buffer for current message (for repetition detection)
	repetitionSuppressed bool   // when true, do not emit more text for this message
}

// NewEventAdapter creates a new event adapter for the given agent name.
func NewEventAdapter(agentName string) *EventAdapter {
	return &EventAdapter{
		agentName: agentName,
	}
}

// detectRepetition returns true if the tail of s contains the same phrase of
// length repetitionWindowSize repeated at least repetitionThreshold times.
// This stops runaway LLM output that repeats the same sentence (e.g. "Wait, I'll just call it.").
func detectRepetition(s string) bool {
	if len(s) < repetitionWindowSize*repetitionThreshold {
		return false
	}
	tail := s[len(s)-repetitionWindowSize:]
	for i := 1; i < repetitionThreshold; i++ {
		start := len(s) - (i+1)*repetitionWindowSize
		if start < 0 {
			return false
		}
		if s[start:start+repetitionWindowSize] != tail {
			return false
		}
	}
	return true
}

// FlushMessage emits a TEXT_MESSAGE_END for any open message.
// Call this when the event stream ends to close the lifecycle.
func (a *EventAdapter) FlushMessage() []interface{} {
	if a.currentMessageID == "" {
		return nil
	}
	msgID := a.currentMessageID
	a.currentMessageID = ""
	a.accumulatedContent = ""
	a.repetitionSuppressed = false
	return []interface{}{
		aguitypes.TextMessageEndMsg{Type: aguitypes.EventTextMessageEnd, MessageID: msgID},
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
				aguitypes.AgentChatMessage{
					Type:    aguitypes.EventTextMessageContent,
					Sender:  "Genie",
					Message: "I have run into my limits (max tool iterations). Do you want me to keep trying? (Reply 'yes' to continue)",
				},
			}
		}

		errMsg := buildErrorMessage(evt.Error)
		messages = append(messages, aguitypes.AgentErrorMsg{
			Type:    aguitypes.EventRunError,
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
			resMsg := aguitypes.AgentToolResponseMsg{
				Type:       aguitypes.EventToolCallResult,
				ToolCallID: choice.Message.ToolID,
				Response:   choice.Message.Content,
			}
			// Detect tool execution errors so the UI can show failure status.
			if strings.HasPrefix(choice.Message.Content, "tool execution error:") ||
				strings.HasPrefix(choice.Message.Content, "Error:") {
				resMsg.Error = fmt.Errorf("%s", choice.Message.Content)
			}
			messages = append(messages, resMsg)
			continue // Don't also emit this as a stream chunk
		}

		// Handle reasoning content (same Delta-first convention as text content).
		reasoningContent := choice.Delta.ReasoningContent
		if reasoningContent == "" {
			reasoningContent = choice.Message.ReasoningContent
		}
		if reasoningContent != "" {
			messages = append(messages, aguitypes.AgentReasoningMsg{
				Type:    aguitypes.EventReasoningMessageContent,
				Content: reasoningContent,
				Delta:   true, // Streaming events are always deltas
			})
		}

		// Handle streaming content. Text is emitted even when tool calls are present
		// so the user sees the LLM's reasoning/preamble alongside tool invocations.
		//
		// Content location follows the OpenAI/Anthropic/Gemini convention:
		//   - Streaming chunks (IsPartial=true): text is in Delta.Content
		//   - Final accumulated response (Done=true, IsPartial=false): text is in Message.Content
		//
		// We read from Delta first (streaming), falling back to Message (final).
		// For final responses, we skip text emission if streaming content was
		// already delivered to avoid showing the full text twice.
		//
		// Repetition detection: when the same phrase is repeated many times at the end
		// of the stream (runaway LLM output), we truncate and close the message so
		// the UI does not hang on an unbounded stream.
		streamContent := choice.Delta.Content
		if streamContent == "" {
			streamContent = choice.Message.Content
		}
		isFinalAccumulated := evt.Done && !evt.IsPartial
		if streamContent != "" && (!isFinalAccumulated || a.currentMessageID == "") {
			// Emit TEXT_MESSAGE_START on the first chunk of a new message.
			if a.currentMessageID == "" {
				a.currentMessageID = uuid.NewString()
				a.accumulatedContent = ""
				a.repetitionSuppressed = false
				messages = append(messages, aguitypes.TextMessageStartMsg{
					Type:      aguitypes.EventTextMessageStart,
					MessageID: a.currentMessageID,
				})
			}

			if !a.repetitionSuppressed {
				a.accumulatedContent += streamContent
				if len(a.accumulatedContent) > maxAccumulatedContentLen {
					a.accumulatedContent = a.accumulatedContent[len(a.accumulatedContent)-maxAccumulatedContentLen:]
				}
				if detectRepetition(a.accumulatedContent) {
					truncMsg := "\n\n[Response truncated due to repetition.]"
					messages = append(messages, aguitypes.AgentStreamChunkMsg{
						Type:      aguitypes.EventTextMessageContent,
						MessageID: a.currentMessageID,
						Content:   truncMsg,
						Delta:     true,
					})
					messages = append(messages, aguitypes.TextMessageEndMsg{
						Type:      aguitypes.EventTextMessageEnd,
						MessageID: a.currentMessageID,
					})
					a.currentMessageID = ""
					a.accumulatedContent = ""
					a.repetitionSuppressed = true
					// Do not emit the current streamContent chunk; we've closed the message.
				} else {
					messages = append(messages, aguitypes.AgentStreamChunkMsg{
						Type:      aguitypes.EventTextMessageContent,
						MessageID: a.currentMessageID,
						Content:   streamContent,
						Delta:     true, // Streaming events are always deltas
					})
				}
			}
		}

		// Handle tool calls (LLM requesting a tool invocation)
		if len(choice.Message.ToolCalls) > 0 {
			// Close any open text message before tool calls.
			if a.currentMessageID != "" {
				messages = append(messages, aguitypes.TextMessageEndMsg{
					Type:      aguitypes.EventTextMessageEnd,
					MessageID: a.currentMessageID,
				})
				a.currentMessageID = ""
				a.accumulatedContent = ""
				a.repetitionSuppressed = false
			}
			for _, toolCall := range choice.Message.ToolCalls {
				messages = append(messages, aguitypes.AgentToolCallMsg{
					Type:       aguitypes.EventToolCallStart,
					ToolName:   toolCall.Function.Name,
					Arguments:  string(toolCall.Function.Arguments),
					ToolCallID: toolCall.ID,
				})
				// Emit TOOL_CALL_ARGS with the full argument payload.
				messages = append(messages, aguitypes.ToolCallArgsMsg{
					Type:       aguitypes.EventToolCallArgs,
					ToolCallID: toolCall.ID,
					Delta:      string(toolCall.Function.Arguments),
				})
				// Emit TOOL_CALL_END since the LLM has finished specifying this call.
				messages = append(messages, aguitypes.ToolCallEndMsg{
					Type:       aguitypes.EventToolCallEnd,
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
					messages = append(messages, aguitypes.TextMessageEndMsg{
						Type:      aguitypes.EventTextMessageEnd,
						MessageID: a.currentMessageID,
					})
					a.currentMessageID = ""
					a.accumulatedContent = ""
					a.repetitionSuppressed = false
				}
			case "error":
				// Agent finished with error - extract details from message content
				errContext := "during execution"
				errMsg := "agent finished with error"
				if choice.Message.Content != "" {
					errMsg = choice.Message.Content
				}
				messages = append(messages, aguitypes.AgentErrorMsg{
					Type:    aguitypes.EventRunError,
					Error:   fmt.Errorf("%s", errMsg),
					Context: errContext,
				})
			case "content_filter":
				messages = append(messages, aguitypes.AgentErrorMsg{
					Type:    aguitypes.EventRunError,
					Error:   fmt.Errorf("response was blocked by content safety filter"),
					Context: "content_filter",
				})
			case "length":
				messages = append(messages, aguitypes.AgentErrorMsg{
					Type:    aguitypes.EventRunError,
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
	logr := logger.GetLogger(ctx).With("fn", "agui.EventAdapter.Start")
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
			if _, isUserInput := rawEvent.(aguitypes.UserInputMsg); isUserInput {
				logr.Info("adapter: forwarding UserInputMsg to TUI")
				outputChan <- rawEvent
			} else {
				// Use blocking send to ensure no events are dropped
				outputChan <- rawEvent
			}
		}
	}
}
