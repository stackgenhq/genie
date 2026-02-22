package agui

import (
	"encoding/json"
	"fmt"
	"strings"

	aguitypes "github.com/appcd-dev/genie/pkg/agui"
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
	case aguitypes.AgentThinkingMsg:
		out = aguiEvent{
			Type:     aguitypes.EventRunStarted,
			ThreadID: threadID,
			RunID:    runID,
		}

	case aguitypes.TextMessageStartMsg:
		out = aguiEvent{
			Type:      aguitypes.EventTextMessageStart,
			MessageID: e.MessageID,
			Role:      "assistant",
		}

	case aguitypes.AgentStreamChunkMsg:
		out = aguiEvent{
			Type:      aguitypes.EventTextMessageContent,
			MessageID: e.MessageID,
			Delta:     e.Content,
		}

	case aguitypes.TextMessageEndMsg:
		out = aguiEvent{
			Type:      aguitypes.EventTextMessageEnd,
			MessageID: e.MessageID,
		}

	case aguitypes.AgentReasoningMsg:
		out = aguiEvent{
			Type:  aguitypes.EventReasoningMessageContent,
			Delta: e.Content,
		}

	case aguitypes.AgentToolCallMsg:
		out = aguiEvent{
			Type:         aguitypes.EventToolCallStart,
			ToolCallID:   e.ToolCallID,
			ToolCallName: e.ToolName,
		}

	case aguitypes.ToolCallArgsMsg:
		out = aguiEvent{
			Type:       aguitypes.EventToolCallArgs,
			ToolCallID: e.ToolCallID,
			Delta:      e.Delta,
		}

	case aguitypes.ToolCallEndMsg:
		out = aguiEvent{
			Type:       aguitypes.EventToolCallEnd,
			ToolCallID: e.ToolCallID,
		}

	case aguitypes.AgentToolResponseMsg:
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
			Type:       aguitypes.EventToolCallResult,
			ToolCallID: e.ToolCallID,
			Content:    content,
			Role:       "tool",
		}

	case aguitypes.AgentCompleteMsg:
		out = aguiEvent{
			Type:     aguitypes.EventRunFinished,
			ThreadID: threadID,
			RunID:    runID,
			Result:   e.Message,
		}

	case aguitypes.AgentErrorMsg:
		errMsg := "unknown error"
		if e.Error != nil {
			errMsg = e.Error.Error()
		}
		out = aguiEvent{
			Type:    aguitypes.EventRunError,
			Message: errMsg,
			Code:    e.Context,
		}

	case aguitypes.StageProgressMsg:
		out = aguiEvent{
			Type:     aguitypes.EventStepStarted,
			StepName: e.Stage,
		}

	case aguitypes.AgentChatMessage:
		out = aguiEvent{
			Type:      aguitypes.EventTextMessageContent,
			MessageID: e.MessageID,
			Delta:     e.Message,
		}

	case aguitypes.LogMsg:
		out = aguiEvent{
			Type: aguitypes.EventCustom,
			Name: "log",
			Value: map[string]interface{}{
				"level":   e.Level.String(),
				"message": e.Message,
				"source":  e.Source,
			},
		}

	case aguitypes.ToolApprovalRequestMsg:
		out = aguiEvent{
			Type:          aguitypes.EventToolApprovalRequest,
			ApprovalID:    e.ApprovalID,
			ToolCallName:  e.ToolName,
			Content:       e.Arguments,
			Justification: e.Justification,
		}

	case aguitypes.ClarificationRequestMsg:
		out = aguiEvent{
			Type:       aguitypes.EventClarificationRequest,
			ApprovalID: e.RequestID,
			Content:    e.Question,
			Message:    e.Context,
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
