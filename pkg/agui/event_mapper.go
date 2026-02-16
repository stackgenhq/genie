package agui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// aguiEvent is the AG-UI wire format for SSE events.
// It matches the BaseEvent interface from the AG-UI spec.
type aguiEvent struct {
	Type      string `json:"type"`
	ThreadID  string `json:"threadId,omitempty"`
	RunID     string `json:"runId,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`

	// Text message fields
	MessageID string `json:"messageId,omitempty"`
	Role      string `json:"role,omitempty"`
	Delta     string `json:"delta,omitempty"`

	// Tool call fields
	ToolCallID   string `json:"toolCallId,omitempty"`
	ToolCallName string `json:"toolCallName,omitempty"`
	Content      string `json:"content,omitempty"`

	// Step fields
	StepName string `json:"stepName,omitempty"`

	// Error fields
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`

	// Custom event fields
	Name  string `json:"name,omitempty"`
	Value any    `json:"value,omitempty"`

	// Result (for RUN_FINISHED)
	Result string `json:"result,omitempty"`

	// HITL approval fields
	ApprovalID    string `json:"approvalId,omitempty"`
	Justification string `json:"justification,omitempty"`
}

// MapEvent converts an internal TUI event to an AG-UI wire-format event.
// The threadID and runID are passed through from the original RunAgentInput.
// Returns the serialized JSON bytes and the AG-UI event type string.
func MapEvent(event interface{}, threadID, runID string) ([]byte, string, error) {
	var out aguiEvent

	switch e := event.(type) {
	case AgentThinkingMsg:
		out = aguiEvent{
			Type:     EventRunStarted,
			ThreadID: threadID,
			RunID:    runID,
		}

	case TextMessageStartMsg:
		out = aguiEvent{
			Type:      EventTextMessageStart,
			MessageID: e.MessageID,
			Role:      "assistant",
		}

	case AgentStreamChunkMsg:
		out = aguiEvent{
			Type:      EventTextMessageContent,
			MessageID: e.MessageID,
			Delta:     e.Content,
		}

	case TextMessageEndMsg:
		out = aguiEvent{
			Type:      EventTextMessageEnd,
			MessageID: e.MessageID,
		}

	case AgentReasoningMsg:
		out = aguiEvent{
			Type:  EventReasoningMessageContent,
			Delta: e.Content,
		}

	case AgentToolCallMsg:
		out = aguiEvent{
			Type:         EventToolCallStart,
			ToolCallID:   e.ToolCallID,
			ToolCallName: e.ToolName,
		}

	case ToolCallArgsMsg:
		out = aguiEvent{
			Type:       EventToolCallArgs,
			ToolCallID: e.ToolCallID,
			Delta:      e.Delta,
		}

	case ToolCallEndMsg:
		out = aguiEvent{
			Type:       EventToolCallEnd,
			ToolCallID: e.ToolCallID,
		}

	case AgentToolResponseMsg:
		content := e.Response
		if e.Error != nil {
			errMsg := e.Error.Error()
			if !strings.HasPrefix(errMsg, "tool execution error: ") {
				content = "tool execution error: " + errMsg
			} else {
				content = errMsg
			}
		}
		out = aguiEvent{
			Type:       EventToolCallResult,
			ToolCallID: e.ToolCallID,
			Content:    content,
			Role:       "tool",
		}

	case AgentCompleteMsg:
		out = aguiEvent{
			Type:     EventRunFinished,
			ThreadID: threadID,
			RunID:    runID,
			Result:   e.Message,
		}

	case AgentErrorMsg:
		errMsg := "unknown error"
		if e.Error != nil {
			errMsg = e.Error.Error()
		}
		out = aguiEvent{
			Type:    EventRunError,
			Message: errMsg,
			Code:    e.Context,
		}

	case StageProgressMsg:
		out = aguiEvent{
			Type:     EventStepStarted,
			StepName: e.Stage,
		}

	case AgentChatMessage:
		out = aguiEvent{
			Type:      EventTextMessageContent,
			MessageID: e.MessageID,
			Delta:     e.Message,
		}

	case LogMsg:
		out = aguiEvent{
			Type: EventCustom,
			Name: "log",
			Value: map[string]interface{}{
				"level":   e.Level.String(),
				"message": e.Message,
				"source":  e.Source,
			},
		}

	case ToolApprovalRequestMsg:
		out = aguiEvent{
			Type:          EventToolApprovalRequest,
			ApprovalID:    e.ApprovalID,
			ToolCallName:  e.ToolName,
			Content:       e.Arguments,
			Justification: e.Justification,
		}

	default:
		return nil, "", fmt.Errorf("unsupported event type: %T", event)
	}

	data, err := json.Marshal(out)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal AG-UI event: %w", err)
	}

	return data, out.Type, nil
}
