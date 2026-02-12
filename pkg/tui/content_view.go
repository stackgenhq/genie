package tui

import tea "github.com/charmbracelet/bubbletea"

//counterfeiter:generate . contentView

// contentView abstracts the active content pane (AgentView pre-completion,
// ChatView post-completion). This interface eliminates repeated m.state.Completed
// routing throughout Model.Update by allowing event handlers to dispatch to a
// single content view regardless of the current UI mode.
//
// Without this interface, every agent event handler in Update would need its own
// if/else branch to route to the correct view, creating duplicated logic and
// increasing the risk of routing bugs.
//
// Note: This interface follows the Bubble Tea presentation layer convention
// rather than the standard ctx+request pattern, since these are synchronous
// UI operations with no I/O or context propagation.
type contentView interface {
	// HandleThinking updates the view to show a thinking/processing indicator.
	HandleThinking(msg AgentThinkingMsg)
	// HandleReasoning appends reasoning/thought content to the view.
	HandleReasoning(msg AgentReasoningMsg)
	// HandleStreamChunk processes a streaming text chunk from the LLM.
	// Returns a tea.Cmd if further processing is needed (e.g., typing animation).
	HandleStreamChunk(msg AgentStreamChunkMsg) tea.Cmd
	// HandleToolCall displays a tool call in the view.
	// Returns a tea.Cmd if further processing is needed.
	HandleToolCall(msg AgentToolCallMsg) tea.Cmd
	// HandleToolResponse updates a previously displayed tool call with its result.
	HandleToolResponse(msg AgentToolResponseMsg)
	// HandleError displays an error message in the view.
	HandleError(context, errMsg string)
}

// activeView returns the content view that should handle agent events based on
// the current application state. Pre-completion, events go to the AgentView
// (progress/streaming display). Post-completion, they go to the ChatView
// (interactive chat with tool cards).
func (m *Model) activeView() contentView {
	if m.state.Completed {
		return &m.chatView
	}
	return &m.agentView
}
