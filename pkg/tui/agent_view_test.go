package tui

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AgentView", func() {
	var (
		view AgentView
	)

	BeforeEach(func() {
		view = NewAgentView(DefaultStyles())
		view.SetDimensions(80, 24)
	})

	Describe("Thinking State", func() {
		It("updates thinking status", func() {
			msg := AgentThinkingMsg{
				AgentName: "Planner",
				Message:   "Planning next steps...",
			}
			updatedView, _ := view.Update(msg)
			view = updatedView

			Expect(view.isThinking).To(BeTrue())
			Expect(view.thinkingMsg).To(Equal("Planning next steps..."))
			Expect(view.agentName).To(Equal("Planner"))
		})

		It("clears thinking status on stream chunk", func() {
			// First set thinking
			view.isThinking = true

			msg := AgentStreamChunkMsg{
				Content: "Hello",
				Delta:   false,
			}
			updatedView, _ := view.Update(msg)
			view = updatedView

			Expect(view.isThinking).To(BeFalse())
			Expect(view.fullContent.String()).To(Equal("Hello"))
		})
	})
})
