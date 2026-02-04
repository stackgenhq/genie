package tui

import (
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
})
