package agui_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	agui "github.com/stackgenhq/genie/pkg/messenger/agui"
	"github.com/stackgenhq/genie/pkg/messenger/agui/aguifakes"
)

var _ = Describe("BackgroundWorker", func() {

	Describe("HandleEvent", func() {
		It("should limit concurrent executions", func() {
			var active int32
			var maxConcurrent int32 = 0
			var wg sync.WaitGroup

			// Handler simulates work and tracks max concurrent executions
			handler := &aguifakes.FakeExpert{}
			handler.HandleStub = func(ctx context.Context, req agui.ChatRequest) {
				current := atomic.AddInt32(&active, 1)
				// Update max observed concurrency
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if current <= max {
						break
					}
					if atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
						break
					}
				}
				time.Sleep(50 * time.Millisecond) // Simulate work
				atomic.AddInt32(&active, -1)
			}

			worker := agui.NewBackgroundWorker(handler, 2)

			// Launch 5 concurrent events
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = worker.HandleEvent(context.Background(), aguitypes.EventRequest{Type: aguitypes.EventTypeWebhook})
				}()
			}

			wg.Wait()
			worker.WaitForCompletion()

			// Max concurrent should not exceed 2 (though with race conditions in test it might be less, but never more)
			// A strictly better check is to ensure that we rejected some requests if we flooded it,
			// but here we just want to ensure we didn't exceed the limit.
			Expect(atomic.LoadInt32(&maxConcurrent)).To(BeNumerically("<=", 2))
		})

		It("should return error when pool is full", func() {
			block := make(chan struct{})
			// Handler blocks until we release it
			handler := &aguifakes.FakeExpert{}
			handler.HandleStub = func(ctx context.Context, req agui.ChatRequest) {
				<-block
			}

			worker := agui.NewBackgroundWorker(handler, 1)

			// 1. Fill the slot
			_, err := worker.HandleEvent(context.Background(), aguitypes.EventRequest{Type: aguitypes.EventTypeWebhook})
			Expect(err).NotTo(HaveOccurred())

			// 2. Try to add another (should fail immediately)
			_, err = worker.HandleEvent(context.Background(), aguitypes.EventRequest{Type: aguitypes.EventTypeWebhook})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("background worker pool is full"))

			// Cleanup
			close(block)
			worker.WaitForCompletion()
		})
	})

	Describe("WaitForCompletion", func() {
		It("should wait for all tasks to finish", func() {
			var finished int32
			handler := &aguifakes.FakeExpert{}
			handler.HandleStub = func(ctx context.Context, req agui.ChatRequest) {
				time.Sleep(50 * time.Millisecond)
				atomic.StoreInt32(&finished, 1)
			}

			worker := agui.NewBackgroundWorker(handler, 1)
			_, err := worker.HandleEvent(context.Background(), aguitypes.EventRequest{Type: aguitypes.EventTypeWebhook})
			Expect(err).NotTo(HaveOccurred())

			// This should block until the handler sleeps and sets finished=1
			worker.WaitForCompletion()

			Expect(atomic.LoadInt32(&finished)).To(Equal(int32(1)))
		})
	})
})
