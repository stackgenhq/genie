package agui_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	agui "github.com/stackgenhq/genie/pkg/messenger/agui"
)

var _ = Describe("serverExpert", func() {

	Describe("jaccardSimilarity (via NewChatHandler dedup helpers)", func() {
		// These are unexported helpers, but we can verify the dedup
		// behaviour through the full Handle path. Specific numeric
		// tests for Jaccard are already covered by events_test.go.
		// Here we focus on the Handle lifecycle.
	})

	Describe("NewChatHandler", func() {
		It("should return a non-nil Expert", func() {
			expert := agui.NewChatHandler(
				func(ctx context.Context) string { return "resume" },
				func(ctx context.Context, msg string, ch chan<- interface{}) error { return nil },
				nil,
			)
			Expect(expert).NotTo(BeNil())
		})

		It("should accept nil resumeFunc gracefully", func() {
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error { return nil },
				nil,
			)
			Expect(expert).NotTo(BeNil())
		})
	})

	Describe("Resume", func() {
		It("should return the value from resumeFunc", func() {
			expert := agui.NewChatHandler(
				func(ctx context.Context) string { return "my capabilities" },
				func(ctx context.Context, msg string, ch chan<- interface{}) error { return nil },
				nil,
			)
			Expect(expert.Resume(context.Background())).To(Equal("my capabilities"))
		})

		It("should return empty string when resumeFunc is nil", func() {
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error { return nil },
				nil,
			)
			Expect(expert.Resume(context.Background())).To(Equal(""))
		})
	})

	Describe("Handle", func() {
		It("should emit RUN_STARTED as first event and RUN_FINISHED as last", func() {
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					// chatFunc that does nothing — just returns
					return nil
				},
				nil,
			)

			eventChan := make(chan interface{}, 100)
			req := agui.ChatRequest{
				ThreadID:  "thread-1",
				RunID:     "run-1",
				Message:   "hello",
				EventChan: eventChan,
			}

			expert.Handle(context.Background(), req)

			// Collect all events
			var events []interface{}
			close(eventChan) // Handle has returned, safe to close for draining
		drain:
			for {
				select {
				case evt, ok := <-eventChan:
					if !ok {
						break drain
					}
					events = append(events, evt)
				default:
					break drain
				}
			}

			Expect(len(events)).To(BeNumerically(">=", 2))

			// First event: RUN_STARTED
			firstEvt, ok := events[0].(aguitypes.AgentThinkingMsg)
			Expect(ok).To(BeTrue(), "first event should be AgentThinkingMsg")
			Expect(firstEvt.Type).To(Equal(aguitypes.EventRunStarted))
			Expect(firstEvt.AgentName).To(Equal("Genie"))

			// Last event: RUN_FINISHED
			lastEvt, ok := events[len(events)-1].(aguitypes.AgentCompleteMsg)
			Expect(ok).To(BeTrue(), "last event should be AgentCompleteMsg")
			Expect(lastEvt.Type).To(Equal(aguitypes.EventRunFinished))
			Expect(lastEvt.Success).To(BeTrue())
		})

		It("should pass the user message to chatFunc", func() {
			var receivedMessage string
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					receivedMessage = msg
					return nil
				},
				nil,
			)

			eventChan := make(chan interface{}, 100)
			expert.Handle(context.Background(), agui.ChatRequest{
				ThreadID:  "t1",
				RunID:     "r1",
				Message:   "what is the weather?",
				EventChan: eventChan,
			})

			Expect(receivedMessage).To(Equal("what is the weather?"))
		})

		It("should inject ThreadID and RunID into the context passed to chatFunc", func() {
			var capturedCtx context.Context
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					capturedCtx = ctx
					return nil
				},
				nil,
			)

			eventChan := make(chan interface{}, 100)
			expert.Handle(context.Background(), agui.ChatRequest{
				ThreadID:  "thread-abc",
				RunID:     "run-xyz",
				Message:   "test",
				EventChan: eventChan,
			})

			Expect(aguitypes.ThreadIDFromContext(capturedCtx)).To(Equal("thread-abc"))
			Expect(aguitypes.RunIDFromContext(capturedCtx)).To(Equal("run-xyz"))
		})

		It("should emit RUN_ERROR when chatFunc returns an error", func() {
			chatErr := errors.New("model unavailable")
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					return chatErr
				},
				nil,
			)

			eventChan := make(chan interface{}, 100)
			expert.Handle(context.Background(), agui.ChatRequest{
				ThreadID:  "t1",
				RunID:     "r1",
				Message:   "test",
				EventChan: eventChan,
			})

			// Collect events
			var events []interface{}
			close(eventChan)
		drain:
			for {
				select {
				case evt, ok := <-eventChan:
					if !ok {
						break drain
					}
					events = append(events, evt)
				default:
					break drain
				}
			}

			// Should have: RUN_STARTED, RUN_ERROR, RUN_FINISHED
			Expect(len(events)).To(BeNumerically(">=", 3))

			// Find the error event
			var foundError bool
			for _, evt := range events {
				if errMsg, ok := evt.(aguitypes.AgentErrorMsg); ok {
					Expect(errMsg.Type).To(Equal(aguitypes.EventRunError))
					Expect(errMsg.Error.Error()).To(Equal("model unavailable"))
					Expect(errMsg.Context).To(Equal("while processing chat message"))
					foundError = true
				}
			}
			Expect(foundError).To(BeTrue(), "expected a RUN_ERROR event")

			// RUN_FINISHED should still be emitted even after error
			lastEvt, ok := events[len(events)-1].(aguitypes.AgentCompleteMsg)
			Expect(ok).To(BeTrue(), "last event should still be RUN_FINISHED")
			Expect(lastEvt.Type).To(Equal(aguitypes.EventRunFinished))
		})

		It("should forward events written by chatFunc to eventChan", func() {
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					ch <- aguitypes.TextMessageStartMsg{
						Type:      aguitypes.EventTextMessageStart,
						MessageID: "msg-42",
					}
					ch <- aguitypes.AgentStreamChunkMsg{
						Type:      aguitypes.EventTextMessageContent,
						MessageID: "msg-42",
						Content:   "Hello from agent",
						Delta:     true,
					}
					ch <- aguitypes.TextMessageEndMsg{
						Type:      aguitypes.EventTextMessageEnd,
						MessageID: "msg-42",
					}
					return nil
				},
				nil,
			)

			eventChan := make(chan interface{}, 100)
			expert.Handle(context.Background(), agui.ChatRequest{
				ThreadID:  "t1",
				RunID:     "r1",
				Message:   "hi",
				EventChan: eventChan,
			})

			// Collect events
			var events []interface{}
			close(eventChan)
		drain:
			for {
				select {
				case evt, ok := <-eventChan:
					if !ok {
						break drain
					}
					events = append(events, evt)
				default:
					break drain
				}
			}

			// Should have: RUN_STARTED + 3 text events + RUN_FINISHED = 5
			Expect(len(events)).To(Equal(5))

			// Verify the text message events are present
			var textStartFound, textContentFound, textEndFound bool
			for _, evt := range events {
				switch e := evt.(type) {
				case aguitypes.TextMessageStartMsg:
					Expect(e.MessageID).To(Equal("msg-42"))
					textStartFound = true
				case aguitypes.AgentStreamChunkMsg:
					Expect(e.Content).To(Equal("Hello from agent"))
					textContentFound = true
				case aguitypes.TextMessageEndMsg:
					Expect(e.MessageID).To(Equal("msg-42"))
					textEndFound = true
				}
			}
			Expect(textStartFound).To(BeTrue())
			Expect(textContentFound).To(BeTrue())
			Expect(textEndFound).To(BeTrue())
		})

		It("should respect context cancellation passed to chatFunc", func() {
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					// Block until context is cancelled
					<-ctx.Done()
					return ctx.Err()
				},
				nil,
			)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			eventChan := make(chan interface{}, 100)
			done := make(chan struct{})

			go func() {
				defer close(done)
				expert.Handle(ctx, agui.ChatRequest{
					ThreadID:  "t1",
					RunID:     "r1",
					Message:   "blocking",
					EventChan: eventChan,
				})
			}()

			Eventually(done, 2*time.Second).Should(BeClosed())

			// Should still have emitted the lifecycle events
			close(eventChan)
			var events []interface{}
		drain:
			for {
				select {
				case evt, ok := <-eventChan:
					if !ok {
						break drain
					}
					events = append(events, evt)
				default:
					break drain
				}
			}

			// At minimum: RUN_STARTED, RUN_ERROR (context cancelled), RUN_FINISHED
			Expect(len(events)).To(BeNumerically(">=", 3))
		})

		It("should handle empty message without panic", func() {
			var receivedMsg string
			expert := agui.NewChatHandler(
				nil,
				func(ctx context.Context, msg string, ch chan<- interface{}) error {
					receivedMsg = msg
					return nil
				},
				nil,
			)

			eventChan := make(chan interface{}, 100)
			expert.Handle(context.Background(), agui.ChatRequest{
				ThreadID:  "t1",
				RunID:     "r1",
				Message:   "",
				EventChan: eventChan,
			})

			Expect(receivedMsg).To(Equal(""))
		})
	})
})
