package tui

import (
	"fmt"
	"strings"
)

// slashCommand defines a TUI-local slash command.
// Slash commands are handled entirely within the TUI and are not sent to the LLM agent.
// Without slash commands, users would have no way to control the TUI itself (clearing chat,
// getting help) without sending unnecessary requests to the LLM.
type slashCommand struct {
	name        string
	description string
	handler     func(cv *ChatView, args string)
}

// slashCommands returns the list of available slash commands.
// This function exists to centralize command definitions and make it easy to add new ones.
func slashCommands() []slashCommand {
	return []slashCommand{
		{
			name:        "/help",
			description: "Show available commands",
			handler:     handleHelp,
		},
		{
			name:        "/clear",
			description: "Clear chat history",
			handler:     handleClear,
		},
	}
}

// handleSlashCommand parses and executes a slash command from user input.
// If the input starts with "/" it is treated as a command and handled locally.
// Returns true if the input was handled as a slash command, false otherwise.
// This function exists to intercept slash commands before they reach the LLM agent,
// providing instant local feedback for TUI-control commands.
func (m *ChatView) handleSlashCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return false
	}

	// Parse command and arguments
	parts := strings.SplitN(trimmed, " ", 2)
	cmdName := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	for _, cmd := range slashCommands() {
		if cmd.name == cmdName {
			// Show the command as a user message
			m.messages = append(m.messages, ChatMessage{Role: "user", Content: trimmed})
			cmd.handler(m, args)
			m.updateViewport()
			return true
		}
	}

	// Unknown command
	m.messages = append(m.messages, ChatMessage{Role: "user", Content: trimmed})
	m.addSystemMessage(fmt.Sprintf("Unknown command `%s`. Type `/help` for available commands.", cmdName))
	m.updateViewport()
	return true
}

// addSystemMessage adds a system-level message to the chat.
// System messages use the "system" role and render with a distinct style.
func (m *ChatView) addSystemMessage(text string) {
	m.messages = append(m.messages, ChatMessage{Role: "system", Content: text})
}

// handleHelp displays available slash commands and usage hints.
func handleHelp(cv *ChatView, _ string) {
	var sb strings.Builder
	sb.WriteString("**Available Commands**\n\n")
	for _, cmd := range slashCommands() {
		sb.WriteString(fmt.Sprintf("`%s` — %s\n", cmd.name, cmd.description))
	}
	sb.WriteString("\n**Tips**\n")
	sb.WriteString("- Ask natural language questions about your generated code\n")
	sb.WriteString("- The agent can read and modify files in your output directory\n")
	sb.WriteString("- Press `Tab` to switch between chat and logs\n")
	cv.addSystemMessage(sb.String())
}

// handleClear clears the chat history and shows a fresh welcome.
func handleClear(cv *ChatView, _ string) {
	cv.messages = []ChatMessage{}
	cv.isStreaming = false
	cv.isLoading = false
	cv.thinkingMsg = ""
	cv.toolCalls = make(map[string]*toolCallState)
	cv.addSystemMessage("Chat cleared. How can I help?")
}
