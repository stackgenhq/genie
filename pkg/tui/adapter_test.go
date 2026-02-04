package tui

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("EventAdapter", func() {
	var adapter *EventAdapter

	BeforeEach(func() {
		adapter = NewEventAdapter("TestAgent")
	})

	Describe("ConvertEvent", func() {
		Context("with streaming content", func() {
			It("should convert to AgentStreamChunkMsg", func() {
				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								Message: model.Message{
									Content: "Hello",
								},
							},
						},
					},
				}

				msgs := adapter.ConvertEvent(evt)
				Expect(msgs).To(HaveLen(1))
				Expect(msgs[0]).To(BeAssignableToTypeOf(AgentStreamChunkMsg{}))
				chunk := msgs[0].(AgentStreamChunkMsg)
				Expect(chunk.Content).To(Equal("Hello"))
				Expect(chunk.Delta).To(BeTrue())
			})
		})

		Context("with tool calls", func() {
			It("should convert to AgentToolCallMsg", func() {
				args := map[string]interface{}{"key": "value"}
				jsonArgs, _ := json.Marshal(args)

				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								Message: model.Message{
									ToolCalls: []model.ToolCall{
										{
											ID: "call_123",
											Function: model.FunctionDefinitionParam{
												Name:      "test_tool",
												Arguments: jsonArgs,
											},
										},
									},
								},
							},
						},
					},
				}

				msgs := adapter.ConvertEvent(evt)
				Expect(msgs).To(HaveLen(1))
				Expect(msgs[0]).To(BeAssignableToTypeOf(AgentToolCallMsg{}))
				toolMsg := msgs[0].(AgentToolCallMsg)
				Expect(toolMsg.ToolName).To(Equal("test_tool"))
				Expect(toolMsg.ToolCallID).To(Equal("call_123"))

				var extractedArgs map[string]interface{}
				json.Unmarshal([]byte(toolMsg.Arguments), &extractedArgs)
				Expect(extractedArgs["key"]).To(Equal("value"))
			})
		})

		Context("with completion error finish reason", func() {
			It("should convert error finish reason to AgentErrorMsg", func() {
				finishReason := "error"
				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								FinishReason: &finishReason,
							},
						},
					},
				}

				msgs := adapter.ConvertEvent(evt)
				Expect(msgs).To(HaveLen(1))
				Expect(msgs[0]).To(BeAssignableToTypeOf(AgentErrorMsg{}))
				errMsg := msgs[0].(AgentErrorMsg)
				Expect(errMsg.Error).To(HaveOccurred())
				Expect(errMsg.Context).To(Equal("during execution"))
			})
		})

		Context("with nil event or response", func() {
			It("should return nil", func() {
				Expect(adapter.ConvertEvent(nil)).To(BeNil())
				Expect(adapter.ConvertEvent(&event.Event{})).To(BeNil())
			})
		})

		Context("with Response.Error", func() {
			It("should convert to AgentErrorMsg with detailed message", func() {
				code := "rate_limit_exceeded"
				evt := &event.Event{
					Response: &model.Response{
						Error: &model.ResponseError{
							Message: "Rate limit exceeded",
							Type:    "api_error",
							Code:    &code,
						},
					},
				}

				msgs := adapter.ConvertEvent(evt)
				Expect(msgs).To(HaveLen(1))
				Expect(msgs[0]).To(BeAssignableToTypeOf(AgentErrorMsg{}))
				errMsg := msgs[0].(AgentErrorMsg)
				Expect(errMsg.Error.Error()).To(ContainSubstring("api_error"))
				Expect(errMsg.Error.Error()).To(ContainSubstring("Rate limit exceeded"))
				Expect(errMsg.Error.Error()).To(ContainSubstring("rate_limit_exceeded"))
				Expect(errMsg.Context).To(Equal("api_error"))
			})
		})

		Context("with content_filter finish reason", func() {
			It("should convert to AgentErrorMsg with content filter message", func() {
				finishReason := "content_filter"
				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								FinishReason: &finishReason,
							},
						},
					},
				}

				msgs := adapter.ConvertEvent(evt)
				Expect(msgs).To(HaveLen(1))
				Expect(msgs[0]).To(BeAssignableToTypeOf(AgentErrorMsg{}))
				errMsg := msgs[0].(AgentErrorMsg)
				Expect(errMsg.Error.Error()).To(ContainSubstring("content safety filter"))
				Expect(errMsg.Context).To(Equal("content_filter"))
			})
		})

		Context("with length finish reason", func() {
			It("should convert to AgentErrorMsg with token limit message", func() {
				finishReason := "length"
				evt := &event.Event{
					Response: &model.Response{
						Choices: []model.Choice{
							{
								FinishReason: &finishReason,
							},
						},
					},
				}

				msgs := adapter.ConvertEvent(evt)
				Expect(msgs).To(HaveLen(1))
				Expect(msgs[0]).To(BeAssignableToTypeOf(AgentErrorMsg{}))
				errMsg := msgs[0].(AgentErrorMsg)
				Expect(errMsg.Error.Error()).To(ContainSubstring("token limit"))
				Expect(errMsg.Context).To(Equal("length_exceeded"))
			})
		})
	})
})
