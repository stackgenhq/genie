package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// EventAdapter converts trpc-agent-go events to TUI messages.
// This adapter exists to bridge the gap between the agent's event stream and the TUI's message system.
// Without this adapter, the TUI would not be able to understand and display agent events.
type EventAdapter struct {
	agentName string
}

// NewEventAdapter creates a new event adapter for the given agent name.
func NewEventAdapter(agentName string) *EventAdapter {
	return &EventAdapter{
		agentName: agentName,
	}
}

// ConvertEvent converts a trpc-agent-go event to one or more TUI messages.
// This method exists to translate agent events into TUI-specific message types.
// Without this conversion, the TUI would receive raw events that it doesn't know how to handle.
//
// This method handles both standard trpc-agent-go events and custom event types used by the granter workflow.
func (EventAdapter) ConvertEvent(evt *event.Event) []tea.Msg {
	if evt == nil || evt.Response == nil {
		return nil
	}

	var messages []tea.Msg

	// Check for API-level errors in the response first
	if evt.Error != nil {
		// Special handling for max tool iterations to provide a user-friendly prompt
		if strings.Contains(evt.Error.Message, "max tool iterations") {
			return []tea.Msg{
				AgentChatMessage{
					Sender:  "Genie",
					Message: "I have run into my limits (max tool iterations). Do you want me to keep trying? (Reply 'yes' to continue)",
				},
			}
		}

		errMsg := buildErrorMessage(evt.Error)
		messages = append(messages, AgentErrorMsg{
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
				ToolCallID: choice.Message.ToolID,
				Response:   choice.Message.Content,
			})
			continue // Don't also emit this as a stream chunk
		}

		// Handle reasoning content
		if choice.Message.ReasoningContent != "" {
			messages = append(messages, AgentReasoningMsg{
				Content: choice.Message.ReasoningContent,
				Delta:   true, // Streaming events are always deltas
			})
		}

		// Handle streaming content (skip for tool call events which have their own rendering)
		if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
			messages = append(messages, AgentStreamChunkMsg{
				Content: choice.Message.Content,
				Delta:   true, // Streaming events are always deltas
			})
		}

		// Handle tool calls (LLM requesting a tool invocation)
		if len(choice.Message.ToolCalls) > 0 {
			for _, toolCall := range choice.Message.ToolCalls {
				// FunctionDefinitionParam.Arguments is []byte containing JSON
				// We just need it as a string
				messages = append(messages, AgentToolCallMsg{
					ToolName:   toolCall.Function.Name,
					Arguments:  string(toolCall.Function.Arguments),
					ToolCallID: toolCall.ID,
				})
			}
		}

		// Handle finish reason (it's a pointer to string)
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finishReason := *choice.FinishReason
			switch finishReason {
			case "error":
				// Agent finished with error - extract details from message content
				errContext := "during execution"
				errMsg := "agent finished with error"
				if choice.Message.Content != "" {
					errMsg = choice.Message.Content
				}
				messages = append(messages, AgentErrorMsg{
					Error:   fmt.Errorf("%s", errMsg),
					Context: errContext,
				})
			case "content_filter":
				messages = append(messages, AgentErrorMsg{
					Error:   fmt.Errorf("response was blocked by content safety filter"),
					Context: "content_filter",
				})
			case "length":
				messages = append(messages, AgentErrorMsg{
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
	inputChan <-chan interface{},
	outputChan chan<- interface{},
) {
	defer close(outputChan)
	for rawEvent := range inputChan {
		// Convert event.Event to TUI messages
		if evt, ok := rawEvent.(*event.Event); ok {
			messages := a.ConvertEvent(evt)
			for _, msg := range messages {
				select {
				case outputChan <- msg:
				default:
					// Output channel full, skip this message
				}
			}
		} else {
			// Pass through other event types as-is
			select {
			case outputChan <- rawEvent:
			default:
				// Output channel full, skip this event
			}
		}
	}
}
