package toolwrap_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConcurrencyMiddleware", func() {
	It("should enforce global concurrency limit", func() {
		mw := toolwrap.ConcurrencyMiddleware(toolwrap.ConcurrencyConfig{
			GlobalLimit: 1,
		})
		var running int32
		var maxRunning int32
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			cur := atomic.AddInt32(&running, 1)
			defer atomic.AddInt32(&running, -1)
			for {
				old := atomic.LoadInt32(&maxRunning)
				if cur <= old || atomic.CompareAndSwapInt32(&maxRunning, old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			return "ok", nil
		})

		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				handler(context.Background(), tc("test")) //nolint:errcheck
			}()
		}
		wg.Wait()
		Expect(atomic.LoadInt32(&maxRunning)).To(Equal(int32(1)))
	})

	It("should cancel on context deadline", func() {
		mw := toolwrap.ConcurrencyMiddleware(toolwrap.ConcurrencyConfig{
			GlobalLimit: 1,
		})
		blocker := make(chan struct{})
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			<-blocker
			return "ok", nil
		})

		go handler(context.Background(), tc("t")) //nolint:errcheck
		time.Sleep(10 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := handler(ctx, tc("t"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cancelled"))
		close(blocker)
	})
})
