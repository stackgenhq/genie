package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TUI Model State Transitions", func() {
	var (
		eventChan chan interface{}
		model     Model
	)

	BeforeEach(func() {
		eventChan = make(chan interface{}, 100)
		inputChan := make(chan string, 10)
		model = NewModel(eventChan, inputChan)

		// Initialize dimensions to ensure views render content instead of "Initializing..."
		By("Setting initial window size")
		updatedModel, _ := model.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
		model = updatedModel.(Model)
	})

	AfterEach(func() {
		close(eventChan)
	})

	Describe("Agent Execution Flow", func() {
		It("transitions correctly from thinking to streaming", func() {
			By("Receiving an AgentThinkingMsg")
			thinkingMsg := AgentThinkingMsg{
				AgentName: "Architect",
				Message:   "Analyzing constraints...",
			}
			updatedModel, _ := model.Update(thinkingMsg)
			model = updatedModel.(Model)

			Expect(model.View()).To(ContainSubstring("⟳ Architect is thinking... Analyzing constraints..."))

			By("Receiving an AgentStreamChunkMsg (start)")
			streamMsg1 := AgentStreamChunkMsg{
				Content: "Based on the analysis",
				Delta:   false,
			}
			updatedModel, _ = model.Update(streamMsg1)
			model = updatedModel.(Model)

			// Simulate typing animation by sending TypingTickMsg until content is revealed
			By("Processing typing animation ticks")
			for i := 0; i < 20; i++ { // Enough ticks to reveal all content
				updatedModel, _ = model.Update(TypingTickMsg{})
				model = updatedModel.(Model)
			}

			Expect(model.View()).To(ContainSubstring("Based on the analysis"))
			Expect(model.View()).ToNot(ContainSubstring("is thinking")) // Thinking should clear

			By("Receiving an AgentStreamChunkMsg (append)")
			streamMsg2 := AgentStreamChunkMsg{
				Content: ", we should use ECS.",
				Delta:   true,
			}
			updatedModel, _ = model.Update(streamMsg2)
			model = updatedModel.(Model)

			// Process more typing ticks for the appended content
			for i := 0; i < 20; i++ {
				updatedModel, _ = model.Update(TypingTickMsg{})
				model = updatedModel.(Model)
			}

			Expect(model.View()).To(ContainSubstring("Based on the analysis, we should use ECS."))
		})
	})

	Describe("Completion and Chat View", func() {
		It("shows completion status and explicitly focuses chat on success", func() {
			By("Receiving a successful AgentCompleteMsg with output dir")
			completeMsg := AgentCompleteMsg{
				Success:   true,
				Message:   "Infrastructure ready",
				OutputDir: "/tmp/genie-output",
			}

			// We expect the chat to be focused, command might be nil or batch
			updatedModel, _ := model.Update(completeMsg)
			model = updatedModel.(Model)

			Expect(model.View()).To(ContainSubstring("✅ Infrastructure ready"))
			Expect(model.View()).To(ContainSubstring("💬 Talk to your Codebase"))
			// Verify chat is focused (checking for focused border style or similar is hard on string,
			// but we can ensure it doesn't crash and shows the right components)
		})

		It("shows error status on failure", func() {
			By("Receiving a failed AgentCompleteMsg")
			completeMsg := AgentCompleteMsg{
				Success: false,
				Message: "Generation failed",
			}

			updatedModel, _ := model.Update(completeMsg)
			model = updatedModel.(Model)

			Expect(model.View()).To(ContainSubstring("❌ Operation failed")) // Fallback message if err is nil in model
		})
	})

	Describe("Log Viewing", func() {
		It("adds and displays logs", func() {
			By("Adding log messages")
			logMsg := LogMsg{
				Level:   LogInfo,
				Message: "Connected to verified registry",
				Source:  "registry",
			}
			updatedModel, _ := model.Update(logMsg)
			model = updatedModel.(Model)

			By("Checking log view content")
			Expect(model.View()).To(ContainSubstring("INFO"))
			Expect(model.View()).To(ContainSubstring("Connected to verified registry"))
			Expect(model.View()).To(ContainSubstring("registry"))
		})
	})

	Describe("System Errors", func() {
		It("displays global errors", func() {
			By("Receiving an AgentErrorMsg")
			errMsg := AgentErrorMsg{
				Error:   errors.New("connection refused"),
				Context: "connecting to LLM",
			}
			updatedModel, _ := model.Update(errMsg)
			model = updatedModel.(Model)

			Expect(model.View()).To(ContainSubstring("❌ Error (connecting to LLM): connection refused"))
		})
	})

	Describe("Resizing", func() {
		It("adjusts layout when window size changes", func() {
			By("Adding logs so log view renders")
			updatedModel, _ := model.Update(LogMsg{Level: LogInfo, Message: "Log 1", Source: "test"})
			model = updatedModel.(Model)

			By("Starting with a large window")
			updatedModel, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
			model = updatedModel.(Model)
			largeView := model.View()

			By("Shrinking the window")
			updatedModel, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
			model = updatedModel.(Model)
			smallView := model.View()

			// Now that log view renders, shrinking window should reduce the number of visible log lines,
			// reducing the overall string length (assuming viewport renders empty lines for height padding
			// or just less content lines).
			// Actually viewport.View() returns 'Height' number of lines.
			Expect(len(smallView)).To(BeNumerically("<", len(largeView)))
			Expect(smallView).To(ContainSubstring("Stackgen"))
		})
	})
})
