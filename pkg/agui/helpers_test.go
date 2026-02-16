package agui_test

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/agui"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Helpers", func() {
	var eventChan chan interface{}

	BeforeEach(func() {
		eventChan = make(chan interface{}, 10)
	})

	Describe("EmitStageProgress", func() {
		It("should emit StageProgressMsg", func(ctx context.Context) {
			agui.EmitStageProgress(ctx, eventChan, "Testing", 1, 4)

			Eventually(eventChan).Should(Receive(BeAssignableToTypeOf(agui.StageProgressMsg{})))

			// Let's do it properly

			// Reset chan
			eventChan = make(chan interface{}, 10)
			agui.EmitStageProgress(ctx, eventChan, "Testing", 1, 4)
			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			progressMsg := receivedMsg.(agui.StageProgressMsg)
			Expect(progressMsg.Stage).To(Equal("Testing"))
			Expect(progressMsg.StageIndex).To(Equal(1))
			Expect(progressMsg.TotalStages).To(Equal(4))
			Expect(progressMsg.Progress).To(Equal(0.25))
		})
	})

	Describe("EmitThinking", func() {
		It("should emit AgentThinkingMsg", func(ctx context.Context) {
			agui.EmitThinking(ctx, eventChan, "Agent", "Thinking...")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			thinkingMsg := receivedMsg.(agui.AgentThinkingMsg)
			Expect(thinkingMsg.AgentName).To(Equal("Agent"))
			Expect(thinkingMsg.Message).To(Equal("Thinking..."))
		})
	})

	Describe("EmitCompletion", func() {
		It("should emit AgentCompleteMsg", func(ctx context.Context) {
			agui.EmitCompletion(ctx, eventChan, true, "Done", "/output")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			completeMsg := receivedMsg.(agui.AgentCompleteMsg)
			Expect(completeMsg.Success).To(BeTrue())
			Expect(completeMsg.Message).To(Equal("Done"))
			Expect(completeMsg.OutputDir).To(Equal("/output"))
		})
	})

	Describe("EmitError", func() {
		It("should emit AgentErrorMsg", func(ctx context.Context) {
			err := fmt.Errorf("oops")
			agui.EmitError(ctx, eventChan, err, "context")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			errorMsg := receivedMsg.(agui.AgentErrorMsg)
			Expect(errorMsg.Error).To(Equal(err))
			Expect(errorMsg.Context).To(Equal("context"))
		})
	})

	Describe("RunGrantWithTUI", func() {
		// This integration helper is hard to test without full TUI setup which requires a terminal.
		// Skipping for now as it involves UI interaction logic.
	})
})
