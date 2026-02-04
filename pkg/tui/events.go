package tui

import (
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// AgentThinkingMsg indicates the agent is processing a request.
// This message triggers the display of a spinner or "thinking" indicator.
type AgentThinkingMsg struct {
	// AgentName is the name of the agent that is thinking
	AgentName string
	// Message is an optional context message (e.g., "Analyzing code...")
	Message string
}

// AgentStreamChunkMsg contains a chunk of streaming text content from the LLM.
// These messages are appended to the viewport as they arrive.
type AgentStreamChunkMsg struct {
	// Content is the text content to append
	Content string
	// Delta indicates if this is a delta update (append) or full content (replace)
	Delta bool
}

// AgentToolCallMsg indicates a tool is being called by the agent.
// This message triggers the display of tool call information in the UI.
type AgentToolCallMsg struct {
	// ToolName is the name of the tool being called
	ToolName string
	// Arguments contains the tool arguments as a JSON string
	Arguments string
	// ToolCallID is a unique identifier for this tool call
	ToolCallID string
}

// AgentToolResponseMsg contains the result of a tool execution.
// This message updates the status of a previously displayed tool call.
type AgentToolResponseMsg struct {
	// ToolCallID matches the ID from AgentToolCallMsg
	ToolCallID string
	// ToolName helps with matching when ID is not available
	ToolName string
	// Response is the tool's response content
	Response string
	// Error indicates if the tool call failed
	Error error
}

// AgentCompleteMsg indicates the agent has finished processing.
// This message triggers the display of completion status and final results.
type AgentCompleteMsg struct {
	// Success indicates if the operation completed successfully
	Success bool
	// Message is an optional completion message
	Message string
	// OutputDir is the directory where artifacts were saved
	OutputDir string
}

// AgentChatMessage represents a message in the chat conversation.
type AgentChatMessage struct {
	Sender  string
	Message string
}

// AgentErrorMsg contains error information from the agent.
// This message triggers the display of error state in the UI.
type AgentErrorMsg struct {
	// Error is the error that occurred
	Error error
	// Context provides additional context about where the error occurred
	Context string
}

// StageProgressMsg indicates progress through the multi-stage workflow.
// This message updates the progress indicator to show which stage is active.
type StageProgressMsg struct {
	// Stage is the current stage name (e.g., "Analyzing", "Architecting", "Generating")
	Stage string
	// Progress is a value between 0.0 and 1.0 indicating overall progress
	Progress float64
	// StageIndex is the zero-based index of the current stage
	StageIndex int
	// TotalStages is the total number of stages
	TotalStages int
}

// Choice represents a model choice from the agent response.
// This is used to accumulate choices from streaming responses.
type Choice struct {
	// Message is the message content
	Message model.Message
	// FinishReason indicates why the generation stopped
	FinishReason string
}

// LogLevel represents the severity level of a log message.
type LogLevel int

const (
	// LogDebug represents debug-level logs
	LogDebug LogLevel = iota
	// LogInfo represents informational logs
	LogInfo
	// LogWarn represents warning logs
	LogWarn
	// LogError represents error logs
	LogError
)

// String returns the string representation of the log level.
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

// LogMsg represents a log entry from the system.
// This message adds a log entry to the scrollable log viewer.
type LogMsg struct {
	// Level is the severity level of the log
	Level LogLevel
	// Message is the log message content
	Message string
	// Source identifies where the log came from (e.g., "analyzer", "architect")
	Source string
}

// TickMsg is sent periodically to update elapsed time.
// This message triggers a re-render to update the time display.
type TickMsg struct {
	// Time is the current time when the tick occurred
	Time time.Time
}

// TypingTickMsg is sent periodically to animate the typing effect.
// This message triggers revealing more characters in the streaming content.
type TypingTickMsg struct{}
