package toolwrap_test

import (
	"context"
	"errors"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("PanicRecoveryMiddleware", func() {
	It("should recover from panics and return an error", func() {
		panicking := func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			panic("boom")
		}
		handler := toolwrap.PanicRecoveryMiddleware().Wrap(panicking)
		result, err := handler(context.Background(), tc("test"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("internal error"))
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(result).To(BeNil())
	})

	It("should pass through successful calls unchanged", func() {
		handler := toolwrap.PanicRecoveryMiddleware().Wrap(passthrough("ok"))
		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})
})

var _ = Describe("LoopDetectionMiddleware", func() {
	It("should block after 3 identical consecutive calls", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough("ok"))
		handler := mw.Wrap(next)
		tc := &toolwrap.ToolCallContext{ToolName: "search", Args: []byte(`{"q":"same"}`)}

		_, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(context.Background(), tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))
		Expect(atomic.LoadInt32(count)).To(Equal(int32(2)))
	})

	It("should allow different args without triggering loop", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		for i := 0; i < 5; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "s", Args: []byte(`{"i":"` + string(rune('a'+i)) + `"}`)}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
	})
})

var _ = Describe("FailureLimitMiddleware", func() {
	It("should block tool after 3 consecutive failures", func() {
		mw := toolwrap.FailureLimitMiddleware()
		handler := mw.Wrap(failing(errors.New("down")))

		for i := 0; i < 3; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "api", Args: []byte(`{"i":` + string(rune('0'+i)) + `}`)}
			_, err := handler(context.Background(), tc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("down"))
		}
		_, err := handler(context.Background(), tc("api"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("consecutively"))
	})

	It("should reset counter on success", func() {
		mw := toolwrap.FailureLimitMiddleware()
		callNum := 0
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			callNum++
			if callNum <= 2 {
				return nil, errors.New("fail")
			}
			return "ok", nil
		})

		handler(context.Background(), tc("api")) //nolint:errcheck
		handler(context.Background(), tc("api")) //nolint:errcheck
		result, err := handler(context.Background(), tc("api"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
		Expect(callNum).To(Equal(3))
	})
})
