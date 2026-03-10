// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/messenger"
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
})

var _ = Describe("EmitAgentMessage (via bus)", func() {
	It("should emit message to registered channel", func() {
		ch := make(chan interface{}, 10)
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformAGUI,
			Channel:  messenger.Channel{ID: "test-emit"},
			Sender:   messenger.Sender{ID: "user"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), origin)
		agui.Register(origin, ch)
		defer agui.Deregister(origin)

		agui.EmitAgentMessage(ctx, "agent", "Hello, user!")

		Expect(ch).To(HaveLen(1))
		evt := <-ch
		msg, ok := evt.(agui.AgentChatMessage)
		Expect(ok).To(BeTrue())
		Expect(msg.Sender).To(Equal("agent"))
		Expect(msg.Message).To(Equal("Hello, user!"))
	})

	It("should not panic with no registered channel", func() {
		Expect(func() {
			agui.EmitAgentMessage(context.Background(), "agent", "test")
		}).NotTo(Panic())
	})

	It("should drop message when channel is full", func() {
		ch := make(chan interface{}) // unbuffered = always full when no receiver
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformAGUI,
			Channel:  messenger.Channel{ID: "test-full"},
			Sender:   messenger.Sender{ID: "user"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), origin)
		agui.Register(origin, ch)
		defer agui.Deregister(origin)

		Expect(func() {
			agui.EmitAgentMessage(ctx, "agent", "dropped")
		}).NotTo(Panic())
	})
})
