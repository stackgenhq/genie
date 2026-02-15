package agui

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Helpers", func() {
	var eventChan chan interface{}

	BeforeEach(func() {
		eventChan = make(chan interface{}, 10)
	})

	Describe("EmitStageProgress", func() {
		It("should emit StageProgressMsg", func() {
			EmitStageProgress(eventChan, "Testing", 1, 4)

			Eventually(eventChan).Should(Receive(BeAssignableToTypeOf(StageProgressMsg{})))

			// Let's do it properly

			// Reset chan
			eventChan = make(chan interface{}, 10)
			EmitStageProgress(eventChan, "Testing", 1, 4)
			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			progressMsg := receivedMsg.(StageProgressMsg)
			Expect(progressMsg.Stage).To(Equal("Testing"))
			Expect(progressMsg.StageIndex).To(Equal(1))
			Expect(progressMsg.TotalStages).To(Equal(4))
			Expect(progressMsg.Progress).To(Equal(0.25))
		})
	})

	Describe("EmitThinking", func() {
		It("should emit AgentThinkingMsg", func() {
			EmitThinking(eventChan, "Agent", "Thinking...")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			thinkingMsg := receivedMsg.(AgentThinkingMsg)
			Expect(thinkingMsg.AgentName).To(Equal("Agent"))
			Expect(thinkingMsg.Message).To(Equal("Thinking..."))
		})
	})

	Describe("EmitCompletion", func() {
		It("should emit AgentCompleteMsg", func() {
			EmitCompletion(eventChan, true, "Done", "/output")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			completeMsg := receivedMsg.(AgentCompleteMsg)
			Expect(completeMsg.Success).To(BeTrue())
			Expect(completeMsg.Message).To(Equal("Done"))
			Expect(completeMsg.OutputDir).To(Equal("/output"))
		})
	})

	Describe("EmitError", func() {
		It("should emit AgentErrorMsg", func() {
			err := fmt.Errorf("oops")
			EmitError(eventChan, err, "context")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			errorMsg := receivedMsg.(AgentErrorMsg)
			Expect(errorMsg.Error).To(Equal(err))
			Expect(errorMsg.Context).To(Equal("context"))
		})
	})

	Describe("RunGrantWithTUI", func() {
		// This integration helper is hard to test without full TUI setup which requires a terminal.
		// Skipping for now as it involves UI interaction logic.
	})
})
