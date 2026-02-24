package toolwrap_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("TimeoutMiddleware", func() {
	It("should pass through when call completes within deadline", func() {
		mw := toolwrap.TimeoutMiddleware(1 * time.Second)
		handler := mw.Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("fast_tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should cancel context when deadline exceeded", func() {
		mw := toolwrap.TimeoutMiddleware(10 * time.Millisecond)
		handler := mw.Wrap(func(ctx context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			select {
			case <-time.After(1 * time.Second):
				return "should not reach", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

		_, err := handler(context.Background(), tc("slow_tool"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("deadline exceeded"))
	})
})

var _ = Describe("PerToolTimeoutMiddleware", func() {
	It("should use per-tool override when available", func() {
		mw := toolwrap.PerToolTimeoutMiddleware(
			map[string]time.Duration{
				"slow_tool": 10 * time.Millisecond,
			}, 5*time.Second)

		handler := mw.Wrap(func(ctx context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			select {
			case <-time.After(1 * time.Second):
				return "should not reach", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

		_, err := handler(context.Background(), tc("slow_tool"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("deadline exceeded"))
	})

	It("should use fallback for unconfigured tools", func() {
		mw := toolwrap.PerToolTimeoutMiddleware(nil, 1*time.Second)
		handler := mw.Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("any_tool"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})
})
