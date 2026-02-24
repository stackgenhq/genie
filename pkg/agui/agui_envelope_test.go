package agui_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
)

var _ = Describe("CloudEvent Envelope", func() {
	Describe("WrapInCloudEvent", func() {
		It("wraps an AG-UI event with correct CloudEvents fields", func() {
			evt := agui.AgentThinkingMsg{
				Type:      agui.EventRunStarted,
				AgentName: "codeowner",
				Message:   "analyzing...",
			}

			ce := agui.WrapInCloudEvent(evt, "genie/reactree/stage-1")

			Expect(ce.SpecVersion).To(Equal("1.0"))
			Expect(ce.ID).NotTo(BeEmpty())
			Expect(ce.Source).To(Equal("genie/reactree/stage-1"))
			Expect(ce.Type).To(Equal("ai.genie.agui.RUN_STARTED"))
			Expect(ce.Time).NotTo(BeZero())
			Expect(ce.Data).To(BeAssignableToTypeOf(agui.AgentThinkingMsg{}))
		})

		It("uses CUSTOM type for non-AGUIEvent values", func() {
			ce := agui.WrapInCloudEvent("plain string", "genie/test")
			Expect(ce.Type).To(Equal("ai.genie.agui.CUSTOM"))
		})

		It("marshals to valid JSON", func() {
			evt := agui.AgentCompleteMsg{
				Type:      agui.EventRunFinished,
				Success:   true,
				Message:   "done",
				OutputDir: "/tmp/out",
			}

			ce := agui.WrapInCloudEvent(evt, "genie/grant")
			data, err := json.Marshal(ce)
			Expect(err).NotTo(HaveOccurred())

			var parsed map[string]interface{}
			Expect(json.Unmarshal(data, &parsed)).To(Succeed())
			Expect(parsed["specversion"]).To(Equal("1.0"))
			Expect(parsed["type"]).To(Equal("ai.genie.agui.RUN_FINISHED"))
			Expect(parsed["source"]).To(Equal("genie/grant"))
			Expect(parsed["data"]).NotTo(BeNil())
		})

		It("maps all event types correctly", func() {
			tests := []struct {
				event    agui.AGUIEvent
				expected string
			}{
				{agui.AgentThinkingMsg{}, "ai.genie.agui.RUN_STARTED"},
				{agui.AgentStreamChunkMsg{}, "ai.genie.agui.TEXT_MESSAGE_CONTENT"},
				{agui.AgentReasoningMsg{}, "ai.genie.agui.REASONING_MESSAGE_CONTENT"},
				{agui.AgentToolCallMsg{}, "ai.genie.agui.TOOL_CALL_START"},
				{agui.AgentToolResponseMsg{}, "ai.genie.agui.TOOL_CALL_RESULT"},
				{agui.AgentCompleteMsg{}, "ai.genie.agui.RUN_FINISHED"},
				{agui.AgentChatMessage{}, "ai.genie.agui.TEXT_MESSAGE_CONTENT"},
				{agui.AgentErrorMsg{}, "ai.genie.agui.RUN_ERROR"},
				{agui.StageProgressMsg{}, "ai.genie.agui.STEP_STARTED"},
				{agui.LogMsg{}, "ai.genie.agui.CUSTOM"},
				{agui.UserInputMsg{}, "ai.genie.agui.CUSTOM"},
			}

			for _, tt := range tests {
				ce := agui.WrapInCloudEvent(tt.event, "test")
				Expect(ce.Type).To(Equal(tt.expected), "mismatch for %T", tt.event)
			}
		})
	})
})
