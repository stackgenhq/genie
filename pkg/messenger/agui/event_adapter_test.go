// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	agui "github.com/stackgenhq/genie/pkg/messenger/agui"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("EventAdapter", func() {
	Describe("ConvertEvent", func() {
		Describe("repetition detection", func() {
			// repetitionWindowSize is 100, repetitionThreshold is 4 in event_adapter.go.
			// We need accumulated content whose last 100*4=400 chars are the same 100-char phrase repeated 4 times.
			repeatedPhrase := "Wait, I'll just call it. Wait, I'll check if I can see the create_agent call that failed. No. ---  "
			for len(repeatedPhrase) < 100 {
				repeatedPhrase += " "
			}
			repeatedPhrase = repeatedPhrase[:100] // exactly 100 chars

			It("truncates and closes message when same phrase is repeated at end of stream", func() {
				adapter := agui.NewEventAdapter("test-agent")
				repeatedContent := repeatedPhrase + repeatedPhrase + repeatedPhrase + repeatedPhrase // 400 chars
				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								Delta:   model.Message{Content: repeatedContent},
								Message: model.Message{Content: repeatedContent, Role: model.RoleAssistant},
							},
						},
					},
				}

				messages := adapter.ConvertEvent(evt)

				// Should have: TEXT_MESSAGE_START, truncation chunk, TEXT_MESSAGE_END (no raw repeated content chunk).
				var startMsg *aguitypes.TextMessageStartMsg
				var truncChunk *aguitypes.AgentStreamChunkMsg
				var endMsg *aguitypes.TextMessageEndMsg
				for _, m := range messages {
					switch v := m.(type) {
					case aguitypes.TextMessageStartMsg:
						startMsg = &v
					case aguitypes.AgentStreamChunkMsg:
						truncChunk = &v
					case aguitypes.TextMessageEndMsg:
						endMsg = &v
					}
				}
				Expect(startMsg).NotTo(BeNil())
				Expect(truncChunk).NotTo(BeNil())
				Expect(truncChunk.Content).To(ContainSubstring("truncated due to repetition"))
				Expect(endMsg).NotTo(BeNil())
			})

			It("does not truncate when content has no repetition", func() {
				adapter := agui.NewEventAdapter("test-agent")
				uniqueContent := "This is a single unique response with no repeated phrase at the end. It has enough length but no repetition."
				for len(uniqueContent) < 400 {
					uniqueContent += " padding"
				}
				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								Delta:   model.Message{Content: uniqueContent},
								Message: model.Message{Content: uniqueContent, Role: model.RoleAssistant},
							},
						},
					},
				}

				messages := adapter.ConvertEvent(evt)

				var chunk *aguitypes.AgentStreamChunkMsg
				for _, m := range messages {
					if v, ok := m.(aguitypes.AgentStreamChunkMsg); ok {
						chunk = &v
						break
					}
				}
				Expect(chunk).NotTo(BeNil())
				Expect(chunk.Content).To(Equal(uniqueContent))
				Expect(chunk.Content).NotTo(ContainSubstring("truncated due to repetition"))
			})

			It("starts a new message after truncation so subsequent events stream normally", func() {
				adapter := agui.NewEventAdapter("test-agent")
				repeatedContent := repeatedPhrase + repeatedPhrase + repeatedPhrase + repeatedPhrase
				evt1 := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								Delta:   model.Message{Content: repeatedContent},
								Message: model.Message{Content: repeatedContent, Role: model.RoleAssistant},
							},
						},
					},
				}
				_ = adapter.ConvertEvent(evt1)

				// Second event: new content. Adapter starts a new message (currentMessageID was cleared).
				// So this content is emitted as a new message, not suppressed.
				evt2 := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								Delta:   model.Message{Content: "Follow-up message after truncation."},
								Message: model.Message{Content: "Follow-up message after truncation.", Role: model.RoleAssistant},
							},
						},
					},
				}
				messages2 := adapter.ConvertEvent(evt2)

				var chunk *aguitypes.AgentStreamChunkMsg
				for _, m := range messages2 {
					if v, ok := m.(aguitypes.AgentStreamChunkMsg); ok {
						chunk = &v
						break
					}
				}
				Expect(chunk).NotTo(BeNil())
				Expect(chunk.Content).To(Equal("Follow-up message after truncation."))
			})
		})
	})
})
