package toolwrap_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RateLimitMiddleware", func() {
	It("should allow calls within rate limit", func() {
		mw := toolwrap.RateLimitMiddleware(toolwrap.RateLimitConfig{
			GlobalRatePerMinute: 60,
		})
		handler := mw.Wrap(passthrough("ok"))

		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should reject calls exceeding per-tool rate limit", func() {
		mw := toolwrap.RateLimitMiddleware(toolwrap.RateLimitConfig{
			PerToolRatePerMinute: map[string]float64{
				"api": 1,
			},
		})
		handler := mw.Wrap(passthrough("ok"))

		_, err := handler(context.Background(), tc("api"))
		Expect(err).NotTo(HaveOccurred())

		_, err = handler(context.Background(), tc("api"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rate-limited"))
	})
})
