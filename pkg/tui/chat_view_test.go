package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ChatView", func() {
	var (
		m         ChatView
		inputChan chan string
		styles    Styles
	)

	BeforeEach(func() {
		inputChan = make(chan string, 1)
		styles = DefaultStyles()
		m = NewChatView(styles, inputChan)
		m.SetDimensions(80, 20)
		m.SetFocus(true)
		// Mock renderer to avoid panic in tests if dimensions not set?
		// SetDimensions handles it.
	})

	Describe("Immediate Feedback", func() {
		It("should show loading state immediately after sending a message", func() {
			// User types a message
			m.textarea.SetValue("Hello Genie")

			// Verify not loading initially
			Expect(m.isLoading).To(BeFalse(), "expected isLoading to be false initially")

			// simulate Enter key
			msg := tea.KeyMsg{Type: tea.KeyEnter}
			updatedModel, _ := m.Update(msg)
			newChatView := updatedModel

			// Verify loading state
			Expect(newChatView.isLoading).To(BeTrue(), "expected isLoading to be true after sending message")

			// Verify viewport content
			view := newChatView.View()
			Expect(view).To(ContainSubstring("Genie is thinking"), "expected view to contain 'Genie is thinking'")
		})
	})

	Describe("Slash Commands", func() {
		It("should handle /help command and show available commands", func() {
			m.textarea.SetValue("/help")
			msg := tea.KeyMsg{Type: tea.KeyEnter}
			updatedModel, _ := m.Update(msg)

			// Should not be loading (slash commands are local)
			Expect(updatedModel.isLoading).To(BeFalse())

			// Should have added a system message with help content
			found := false
			for _, chatMsg := range updatedModel.messages {
				if chatMsg.Role == "system" && strings.Contains(chatMsg.Content, "Available Commands") {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "expected a system message containing 'Available Commands'")
		})

		It("should handle /clear command and clear chat history", func() {
			// Add some messages first
			m.AddMessage("user", "test message")
			m.AddMessage("Genie", "test response")

			m.textarea.SetValue("/clear")
			msg := tea.KeyMsg{Type: tea.KeyEnter}
			updatedModel, _ := m.Update(msg)

			// Should have cleared messages (only system "cleared" message remains)
			Expect(updatedModel.isLoading).To(BeFalse())
			view := updatedModel.View()
			Expect(view).To(ContainSubstring("Chat cleared"))
		})

		It("should handle unknown slash commands gracefully", func() {
			m.textarea.SetValue("/unknown")
			msg := tea.KeyMsg{Type: tea.KeyEnter}
			updatedModel, _ := m.Update(msg)

			view := updatedModel.View()
			Expect(view).To(ContainSubstring("Unknown command"))
		})

		It("should not send slash commands to the agent", func() {
			m.textarea.SetValue("/help")
			msg := tea.KeyMsg{Type: tea.KeyEnter}
			m.Update(msg)

			// inputChan should be empty — command was handled locally
			Expect(inputChan).ToNot(Receive())
		})
	})

	Describe("Tool Call Visualization", func() {
		It("should display tool call cards", func() {
			m.AddToolCall(AgentToolCallMsg{
				ToolName:   "read_file",
				Arguments:  `{"file_name":"main.tf"}`,
				ToolCallID: "tc-1",
			})

			view := m.View()
			Expect(view).To(ContainSubstring("read_file"))
			Expect(view).To(ContainSubstring("main.tf"))
		})

		It("should update tool call status on response", func() {
			m.AddToolCall(AgentToolCallMsg{
				ToolName:   "read_file",
				Arguments:  `{"file_name":"main.tf"}`,
				ToolCallID: "tc-1",
			})

			m.UpdateToolCall(AgentToolResponseMsg{
				ToolCallID: "tc-1",
				ToolName:   "read_file",
				Response:   "line1\nline2\nline3",
			})

			// Should show success indicator and result summary
			found := false
			for _, msg := range m.messages {
				if msg.Role == "tool" {
					Expect(msg.Content).To(ContainSubstring("✓"))
					Expect(msg.Content).To(ContainSubstring("3 lines read"))
					found = true
				}
			}
			Expect(found).To(BeTrue(), "expected to find a tool message")
		})
	})

	Describe("Contextual Thinking", func() {
		It("should show contextual thinking message", func() {
			m.SetThinking("Analyzing main.tf...")

			Expect(m.isLoading).To(BeTrue())
			view := m.View()
			Expect(view).To(ContainSubstring("Analyzing main.tf"))
		})

		It("should clear thinking message when loading stops", func() {
			m.SetThinking("Reading files...")
			m.SetLoading(false)

			Expect(m.isLoading).To(BeFalse())
			Expect(m.thinkingMsg).To(Equal(""))
		})
	})
})
