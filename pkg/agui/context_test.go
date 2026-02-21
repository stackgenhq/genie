package agui_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/agui"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Context Helpers", func() {
	Describe("WithThreadID / ThreadIDFromContext", func() {
		It("should store and retrieve thread ID", func() {
			ctx := agui.WithThreadID(context.Background(), "thread-123")
			Expect(agui.ThreadIDFromContext(ctx)).To(Equal("thread-123"))
		})

		It("should return empty string when not set", func() {
			Expect(agui.ThreadIDFromContext(context.Background())).To(Equal(""))
		})
	})

	Describe("WithRunID / RunIDFromContext", func() {
		It("should store and retrieve run ID", func() {
			ctx := agui.WithRunID(context.Background(), "run-456")
			Expect(agui.RunIDFromContext(ctx)).To(Equal("run-456"))
		})

		It("should return empty string when not set", func() {
			Expect(agui.RunIDFromContext(context.Background())).To(Equal(""))
		})
	})

	Describe("WithEventChan / EventChanFromContext", func() {
		It("should store and retrieve event channel", func() {
			ch := make(chan<- interface{}, 1)
			ctx := agui.WithEventChan(context.Background(), ch)
			Expect(agui.EventChanFromContext(ctx)).NotTo(BeNil())
		})

		It("should return nil when not set", func() {
			Expect(agui.EventChanFromContext(context.Background())).To(BeNil())
		})
	})
})

var _ = Describe("EmitAgentMessage", func() {
	It("should emit message to event channel", func() {
		ch := make(chan interface{}, 10)
		agui.EmitAgentMessage(context.Background(), ch, "agent", "Hello, user!")

		Expect(ch).To(HaveLen(1))
		evt := <-ch
		msg, ok := evt.(agui.AgentChatMessage)
		Expect(ok).To(BeTrue())
		Expect(msg.Sender).To(Equal("agent"))
		Expect(msg.Message).To(Equal("Hello, user!"))
	})

	It("should not panic with nil channel", func() {
		Expect(func() {
			agui.EmitAgentMessage(context.Background(), nil, "agent", "test")
		}).NotTo(Panic())
	})

	It("should drop message when channel is full", func() {
		ch := make(chan interface{}) // unbuffered = always full when no receiver
		Expect(func() {
			agui.EmitAgentMessage(context.Background(), ch, "agent", "dropped")
		}).NotTo(Panic())
	})
})
