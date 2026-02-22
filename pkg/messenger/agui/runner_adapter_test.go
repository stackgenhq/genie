package agui_test

import (
	"context"
	"errors"
	"sync"
	"time"

	agui "github.com/appcd-dev/genie/pkg/messenger/agui"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("RunnerAdapter", func() {
	Describe("NewRunner", func() {
		It("returns a non-nil runner", func() {
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				return nil
			}
			runner := agui.NewRunner(chatFunc)
			Expect(runner).NotTo(BeNil())
		})
	})

	Describe("Run", func() {
		It("forwards *event.Event from chatFunc to the returned channel", func() {
			evt := &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{
						{Message: model.Message{Content: "hello"}},
					},
				},
			}
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				ch <- evt
				return nil
			}

			runner := agui.NewRunner(chatFunc)
			ch, err := runner.Run(context.Background(), "user1", "sess1", model.Message{Content: "hi"})
			Expect(err).NotTo(HaveOccurred())

			var received []*event.Event
			for e := range ch {
				received = append(received, e)
			}
			Expect(received).To(HaveLen(1))
			Expect(received[0].Response.Choices[0].Message.Content).To(Equal("hello"))
		})

		It("skips non-event types without panicking", func() {
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				ch <- "some string"
				ch <- 42
				ch <- &event.Event{Response: &model.Response{
					Choices: []model.Choice{{Message: model.Message{Content: "real"}}},
				}}
				return nil
			}

			runner := agui.NewRunner(chatFunc)
			ch, err := runner.Run(context.Background(), "u", "s", model.Message{Content: "test"})
			Expect(err).NotTo(HaveOccurred())

			var received []*event.Event
			for e := range ch {
				received = append(received, e)
			}
			Expect(received).To(HaveLen(1))
			Expect(received[0].Response.Choices[0].Message.Content).To(Equal("real"))
		})

		It("emits an error event when chatFunc returns an error (blind spot #2)", func() {
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				return errors.New("pipeline failed")
			}

			runner := agui.NewRunner(chatFunc)
			ch, err := runner.Run(context.Background(), "u", "s", model.Message{Content: "test"})
			Expect(err).NotTo(HaveOccurred())

			var received []*event.Event
			for e := range ch {
				received = append(received, e)
			}
			Expect(received).To(HaveLen(1))
			Expect(received[0].Response.Error).NotTo(BeNil())
			Expect(received[0].Response.Error.Message).To(ContainSubstring("pipeline failed"))
			Expect(received[0].Response.Error.Type).To(Equal("runner_error"))
		})

		It("does not panic when chatFunc writes after returning (blind spot #1 regression)", func() {
			// This test is specifically designed to detect the race condition
			// where rawChan closing races with chatFunc writing.
			// With -race flag, this will catch data races.
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				// Rapidly write many events — if the channel is prematurely
				// closed, this will panic with "send on closed channel".
				for i := 0; i < 50; i++ {
					ch <- &event.Event{
						Response: &model.Response{
							Choices: []model.Choice{{Message: model.Message{Content: "evt"}}},
						},
					}
				}
				return nil
			}

			runner := agui.NewRunner(chatFunc)
			ch, err := runner.Run(context.Background(), "u", "s", model.Message{Content: "test"})
			Expect(err).NotTo(HaveOccurred())

			count := 0
			for range ch {
				count++
			}
			Expect(count).To(Equal(50))
		})

		It("stops forwarding when context is cancelled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				for i := 0; i < 100; i++ {
					select {
					case ch <- &event.Event{Response: &model.Response{
						Choices: []model.Choice{{Message: model.Message{Content: "evt"}}},
					}}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}

			runner := agui.NewRunner(chatFunc)
			ch, err := runner.Run(ctx, "u", "s", model.Message{Content: "test"})
			Expect(err).NotTo(HaveOccurred())

			// Read one event then cancel
			<-ch
			cancel()

			// Drain remaining buffered events and verify the channel
			// eventually closes (doesn't hang). Use a goroutine + timeout
			// to avoid the test blocking forever on a regression.
			done := make(chan struct{})
			go func() {
				defer close(done)
				for range ch {
					// drain
				}
			}()
			Eventually(done, 5*time.Second).Should(BeClosed())
		})

		It("handles concurrent runs without races (race test)", func() {
			chatFunc := func(ctx context.Context, msg string, ch chan<- interface{}) error {
				for i := 0; i < 10; i++ {
					ch <- &event.Event{Response: &model.Response{
						Choices: []model.Choice{{Message: model.Message{Content: msg}}},
					}}
				}
				return nil
			}

			runner := agui.NewRunner(chatFunc)

			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					defer GinkgoRecover()

					ch, err := runner.Run(context.Background(), "user", "sess",
						model.Message{Content: "concurrent"})
					Expect(err).NotTo(HaveOccurred())

					count := 0
					for range ch {
						count++
					}
					Expect(count).To(Equal(10))
				}(i)
			}
			wg.Wait()
		})
	})

	Describe("Close", func() {
		It("returns nil (no-op)", func() {
			runner := agui.NewRunner(func(ctx context.Context, msg string, ch chan<- interface{}) error {
				return nil
			})
			Expect(runner.Close()).To(Succeed())
		})
	})
})
