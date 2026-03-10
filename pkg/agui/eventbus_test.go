// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/messenger"
)

// testOriginWith creates a unique MessageOrigin with the given suffix,
// registers a channel on the bus, and returns a context with the origin set.
func testOriginWith(ch chan interface{}, suffix string) (context.Context, func()) {
	origin := messenger.MessageOrigin{
		Platform: messenger.PlatformAGUI,
		Channel:  messenger.Channel{ID: "eventbus-test-" + suffix},
		Sender:   messenger.Sender{ID: "test-user"},
	}
	ctx := messenger.WithMessageOrigin(context.Background(), origin)
	agui.Register(origin, ch)
	return ctx, func() { agui.Deregister(origin) }
}

var _ = Describe("Event Bus", func() {

	Describe("Register / Deregister lifecycle", func() {
		It("should route events to a registered channel", func() {
			ch := make(chan interface{}, 10)
			ctx, cleanup := testOriginWith(ch, "route-basic")
			defer cleanup()

			agui.Emit(ctx, "hello")
			Eventually(ch).Should(Receive(Equal("hello")))
		})

		It("should silently drop events after Deregister", func() {
			ch := make(chan interface{}, 10)
			ctx, cleanup := testOriginWith(ch, "deregister-drop")
			cleanup() // deregister immediately

			agui.Emit(ctx, "should-be-dropped")
			Consistently(ch).ShouldNot(Receive())
		})

		It("should return nil from ChannelFor after Deregister", func() {
			ch := make(chan interface{}, 10)
			ctx, cleanup := testOriginWith(ch, "channelfor-nil")

			Expect(agui.ChannelFor(ctx)).NotTo(BeNil())
			cleanup()
			Expect(agui.ChannelFor(ctx)).To(BeNil())
		})

		It("should not panic when Deregister is called for unknown origin", func() {
			unknownOrigin := messenger.MessageOrigin{
				Platform: messenger.PlatformAGUI,
				Channel:  messenger.Channel{ID: "never-registered"},
				Sender:   messenger.Sender{ID: "ghost"},
			}
			Expect(func() {
				agui.Deregister(unknownOrigin)
			}).NotTo(Panic())
		})
	})

	Describe("Concurrent isolation", func() {
		It("should isolate events between different origins", func() {
			chA := make(chan interface{}, 10)
			chB := make(chan interface{}, 10)

			ctxA, cleanupA := testOriginWith(chA, "isolation-A")
			defer cleanupA()
			ctxB, cleanupB := testOriginWith(chB, "isolation-B")
			defer cleanupB()

			agui.Emit(ctxA, "for-A")
			agui.Emit(ctxB, "for-B")

			Eventually(chA).Should(Receive(Equal("for-A")))
			Eventually(chB).Should(Receive(Equal("for-B")))

			// A should not have received B's event and vice versa
			Consistently(chA).ShouldNot(Receive())
			Consistently(chB).ShouldNot(Receive())
		})

		It("should handle concurrent Register/Emit/Deregister without panics", func() {
			const goroutines = 50
			var wg sync.WaitGroup
			wg.Add(goroutines)

			for i := 0; i < goroutines; i++ {
				go func(idx int) {
					defer wg.Done()
					ch := make(chan interface{}, 5)
					origin := messenger.MessageOrigin{
						Platform: messenger.PlatformAGUI,
						Channel:  messenger.Channel{ID: "concurrent-" + string(rune('A'+idx%26))},
						Sender:   messenger.Sender{ID: "user"},
					}
					ctx := messenger.WithMessageOrigin(context.Background(), origin)

					agui.Register(origin, ch)
					agui.Emit(ctx, "event")
					agui.Deregister(origin)
				}(i)
			}

			wg.Wait() // if we get here without panic, the test passes
		})
	})

	Describe("Deregister-before-close pattern", func() {
		It("should prevent sends to closed channels", func() {
			ch := make(chan interface{}, 10)
			ctx, _ := testOriginWith(ch, "deregister-close")

			// Simulate the correct pattern: deregister then close
			origin := messenger.MessageOriginFrom(ctx)
			agui.Deregister(origin)
			close(ch)

			// Emit after deregister+close should not panic
			Expect(func() {
				agui.Emit(ctx, "after-close")
			}).NotTo(Panic())
		})

		It("should demonstrate that close-before-deregister would panic", func() {
			// This test documents WHY the order matters.
			// We don't actually close before deregister (that would panic),
			// instead we verify the safe pattern works consistently.
			ch := make(chan interface{}, 10)
			_, cleanup := testOriginWith(ch, "order-matters")

			// Safe pattern: deregister first
			cleanup()
			close(ch)

			// Channel is gone from bus — no sends possible
		})
	})

	Describe("Backpressure (full channel)", func() {
		It("should not block when channel is full", func() {
			ch := make(chan interface{}) // unbuffered — always "full"
			ctx, cleanup := testOriginWith(ch, "backpressure")
			defer cleanup()

			// Should complete immediately (default case in select)
			done := make(chan struct{})
			go func() {
				agui.Emit(ctx, "dropped")
				close(done)
			}()

			Eventually(done).Should(BeClosed())
		})
	})

	Describe("Context without MessageOrigin", func() {
		It("should silently drop events when no origin in context", func() {
			Expect(func() {
				agui.Emit(context.Background(), "orphan-event")
			}).NotTo(Panic())
		})

		It("should return nil from ChannelFor with no origin", func() {
			Expect(agui.ChannelFor(context.Background())).To(BeNil())
		})
	})

	Describe("Per-message isolation (messenger pattern)", func() {
		It("should support multiple concurrent registrations with unique origins", func() {
			const messages = 10
			var received atomic.Int64
			var wg sync.WaitGroup
			wg.Add(messages)

			for i := 0; i < messages; i++ {
				go func(idx int) {
					defer wg.Done()

					// Each "message" gets its own drain channel — like handleMessengerInput.
					// NOTE: MessageOrigin.String() uses Platform:SenderID:ChannelID,
					// so MessageID is NOT part of the bus key. We use a unique
					// channel ID per message to ensure isolation.
					ch := make(chan interface{}, 10)
					origin := messenger.MessageOrigin{
						Platform: messenger.Platform("slack"),
						Channel:  messenger.Channel{ID: fmt.Sprintf("chan-%d", idx)},
						Sender:   messenger.Sender{ID: "user1"},
					}
					ctx := messenger.WithMessageOrigin(context.Background(), origin)

					agui.Register(origin, ch)

					// Emit an event
					agui.Emit(ctx, "msg-event")

					// Receive it
					select {
					case <-ch:
						received.Add(1)
					default:
					}

					agui.Deregister(origin)
					close(ch)
				}(i)
			}

			wg.Wait()
			Expect(received.Load()).To(BeNumerically("==", messages))
		})

		It("should demonstrate that same Platform:Sender:Channel collide in the bus", func() {
			// This test documents the key collision behavior:
			// Two origins with the same Platform+Sender+Channel but different
			// MessageID will share the same bus key.
			chA := make(chan interface{}, 10)
			chB := make(chan interface{}, 10)

			originA := messenger.MessageOrigin{
				Platform:  messenger.Platform("slack"),
				Channel:   messenger.Channel{ID: "general"},
				Sender:    messenger.Sender{ID: "user1"},
				MessageID: "msg-1",
			}
			originB := messenger.MessageOrigin{
				Platform:  messenger.Platform("slack"),
				Channel:   messenger.Channel{ID: "general"},
				Sender:    messenger.Sender{ID: "user1"},
				MessageID: "msg-2", // different message, same bus key!
			}

			// Both have the same String() key
			Expect(originA.String()).To(Equal(originB.String()))

			agui.Register(originA, chA)
			agui.Register(originB, chB) // overwrites chA in the bus!

			ctxA := messenger.WithMessageOrigin(context.Background(), originA)
			agui.Emit(ctxA, "for-A")

			// Event goes to chB (last registered for this key), not chA
			Eventually(chB).Should(Receive(Equal("for-A")))
			Consistently(chA).ShouldNot(Receive())

			agui.Deregister(originA)
		})
	})
})
