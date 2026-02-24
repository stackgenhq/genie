package agui_test

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/messenger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// testOrigin creates a unique MessageOrigin and registers the channel on the bus.
func testOrigin(ch chan interface{}, id string) (context.Context, func()) {
	origin := messenger.MessageOrigin{
		Platform: messenger.PlatformAGUI,
		Channel:  messenger.Channel{ID: id},
		Sender:   messenger.Sender{ID: "test"},
	}
	ctx := messenger.WithMessageOrigin(context.Background(), origin)
	agui.Register(origin, ch)
	return ctx, func() { agui.Deregister(origin) }
}

var _ = Describe("Helpers", func() {
	var eventChan chan interface{}

	BeforeEach(func() {
		eventChan = make(chan interface{}, 10)
	})

	Describe("EmitStageProgress", func() {
		It("should emit StageProgressMsg", func() {
			ctx, cleanup := testOrigin(eventChan, "stage-progress-1")
			defer cleanup()
			agui.EmitStageProgress(ctx, "Testing", 1, 4)

			Eventually(eventChan).Should(Receive(BeAssignableToTypeOf(agui.StageProgressMsg{})))

			// Reset chan
			eventChan = make(chan interface{}, 10)
			ctx, cleanup2 := testOrigin(eventChan, "stage-progress-2")
			defer cleanup2()

			agui.EmitStageProgress(ctx, "Testing", 1, 4)
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
		It("should emit AgentThinkingMsg", func() {
			ctx, cleanup := testOrigin(eventChan, "thinking")
			defer cleanup()
			agui.EmitThinking(ctx, "Agent", "Thinking...")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			thinkingMsg := receivedMsg.(agui.AgentThinkingMsg)
			Expect(thinkingMsg.AgentName).To(Equal("Agent"))
			Expect(thinkingMsg.Message).To(Equal("Thinking..."))
		})
	})

	Describe("EmitCompletion", func() {
		It("should emit AgentCompleteMsg", func() {
			ctx, cleanup := testOrigin(eventChan, "completion")
			defer cleanup()
			agui.EmitCompletion(ctx, true, "Done", "/output")

			var receivedMsg interface{}
			Eventually(eventChan).Should(Receive(&receivedMsg))

			completeMsg := receivedMsg.(agui.AgentCompleteMsg)
			Expect(completeMsg.Success).To(BeTrue())
			Expect(completeMsg.Message).To(Equal("Done"))
			Expect(completeMsg.OutputDir).To(Equal("/output"))
		})
	})

	Describe("EmitError", func() {
		It("should emit AgentErrorMsg", func() {
			ctx, cleanup := testOrigin(eventChan, "error")
			defer cleanup()
			err := fmt.Errorf("oops")
			agui.EmitError(ctx, err, "context")

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
