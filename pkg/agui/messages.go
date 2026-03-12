// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui

// Event types for AG-UI wire format (SSE)
const (
	EventRunStarted              = "RUN_STARTED"
	EventRunFinished             = "RUN_FINISHED"
	EventRunError                = "RUN_ERROR"
	EventTextMessageStart        = "TEXT_MESSAGE_START"
	EventTextMessageContent      = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd          = "TEXT_MESSAGE_END"
	EventReasoningMessageContent = "REASONING_MESSAGE_CONTENT"
	EventToolCallStart           = "TOOL_CALL_START"
	EventToolCallArgs            = "TOOL_CALL_ARGS"
	EventToolCallEnd             = "TOOL_CALL_END"
	EventToolCallResult          = "TOOL_CALL_RESULT"
	EventStepStarted             = "STEP_STARTED"
	EventStepFinished            = "STEP_FINISHED" // Not currently used but good for completeness
	EventCustom                  = "CUSTOM"
	EventToolApprovalRequest     = "TOOL_APPROVAL_REQUEST"
	EventClarificationRequest    = "CLARIFICATION_REQUEST"
)

// AGUIEvent is a common interface for all AG-UI events.
type AGUIEvent interface {
	AGUIType() string
}

// Internal TUI message types

// AgentThinkingMsg indicates the agent is starting work or thinking.
type AgentThinkingMsg struct {
	Type      string
	AgentName string
	Message   string
}

func (m AgentThinkingMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventRunStarted
}

// TextMessageStartMsg indicates the start of a text message block.
type TextMessageStartMsg struct {
	Type      string
	MessageID string
}

func (m TextMessageStartMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventTextMessageStart
}

// AgentStreamChunkMsg carries a chunk of text content.
type AgentStreamChunkMsg struct {
	Type      string
	MessageID string
	Content   string
	Delta     bool
}

func (m AgentStreamChunkMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventTextMessageContent
}

// TextMessageEndMsg indicates the end of a text message block.
type TextMessageEndMsg struct {
	Type      string
	MessageID string
}

func (m TextMessageEndMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventTextMessageEnd
}

// AgentReasoningMsg carries reasoning content (Chain of Thought).
type AgentReasoningMsg struct {
	Type    string
	Content string
	Delta   bool
}

func (m AgentReasoningMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventReasoningMessageContent
}

// AgentToolCallMsg indicates a tool call is starting.
type AgentToolCallMsg struct {
	Type       string
	ToolName   string
	Arguments  string
	ToolCallID string
}

func (m AgentToolCallMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventToolCallStart
}

// ToolCallArgsMsg carries streaming arguments for a tool call.
type ToolCallArgsMsg struct {
	Type       string
	ToolCallID string
	Delta      string
}

func (m ToolCallArgsMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventToolCallArgs
}

// ToolCallEndMsg indicates a tool call definition is complete.
type ToolCallEndMsg struct {
	Type       string
	ToolCallID string
}

func (m ToolCallEndMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventToolCallEnd
}

// AgentToolResponseMsg carries the result of a tool execution.
type AgentToolResponseMsg struct {
	Type       string
	ToolCallID string
	ToolName   string
	Response   string
	Error      error
}

func (m AgentToolResponseMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventToolCallResult
}

// AgentCompleteMsg indicates the run has finished.
type AgentCompleteMsg struct {
	Type      string
	Success   bool
	Message   string
	OutputDir string
}

func (m AgentCompleteMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventRunFinished
}

// AgentErrorMsg indicates an error occurred.
type AgentErrorMsg struct {
	Type    string
	Error   error
	Context string // e.g., "init", "run", "tool_execution"
}

func (m AgentErrorMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventRunError
}

// StageProgressMsg indicates progress in a multi-stage workflow.
type StageProgressMsg struct {
	Type        string
	Stage       string
	Progress    float64
	StageIndex  int
	TotalStages int
}

func (m StageProgressMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventStepStarted
}

// AgentChatMessage is a complete chat message (non-streaming).
type AgentChatMessage struct {
	Type      string
	MessageID string
	Sender    string
	Message   string
}

func (m AgentChatMessage) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventTextMessageContent
}

// LogLevel defines the severity of a log entry.
type LogLevel int

const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
)

func (l LogLevel) String() string {
	switch l {
	case LogDebug:
		return "DEBUG"
	case LogInfo:
		return "INFO"
	case LogWarn:
		return "WARN"
	case LogError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// LogMsg carries a log entry.
type LogMsg struct {
	Type    string
	Level   LogLevel
	Message string
	Source  string
}

func (m LogMsg) AGUIType() string { return EventCustom }

// ToolApprovalRequestMsg requests user approval for a tool call.
type ToolApprovalRequestMsg struct {
	Type          string
	ApprovalID    string
	ToolName      string
	Arguments     string
	Justification string // why the LLM is making this tool call
	AutoApproved  bool   // true if this tool call was automatically approved (skip human challenge)
}

func (m ToolApprovalRequestMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventToolApprovalRequest
}

// ClarificationRequestMsg asks the user a clarifying question.
type ClarificationRequestMsg struct {
	Type      string
	RequestID string
	Question  string
	Context   string // optional: why the LLM needs this information
}

func (m ClarificationRequestMsg) AGUIType() string {
	if m.Type != "" {
		return m.Type
	}
	return EventClarificationRequest
}

// UserInputMsg represents input from the user (e.g. from stdin).
type UserInputMsg struct {
	Content string
}

func (m UserInputMsg) AGUIType() string { return EventCustom }

// UserActionRequiredMsg signals that the user must perform an action (e.g. OAuth login,
// confirmation, opening a URL). It is emitted as a CUSTOM AG-UI event with the name
// "user_action_required". The chat UI renders a native card with action buttons.
//
// Supported Action values:
//   - "oauth_login" — user must sign in via OAuth
//   - "confirm"     — user must confirm/approve something
//   - "open_url"    — user should open a URL in their browser
type UserActionRequiredMsg struct {
	Action  string // action type: "oauth_login", "confirm", "open_url"
	Service string // service name (e.g. "stackgen", "github")
	URL     string // URL the user should visit
	Message string // human-readable message describing the action
}

func (m UserActionRequiredMsg) AGUIType() string { return EventCustom }
