package toolwrap_test

import (
	"context"
	"errors"
	"time"

	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CircuitBreakerMiddleware", func() {
	It("should open circuit after threshold failures", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 2,
			OpenDuration:     1 * time.Second,
		})
		handler := mw.Wrap(failing(errors.New("down")))

		for i := 0; i < 2; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "api", Args: []byte(`{"i":` + string(rune('0'+i)) + `}`)}
			_, err := handler(context.Background(), tc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("down"))
		}

		_, err := handler(context.Background(), tc("api"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("circuit"))
	})

	It("should allow calls when circuit is closed", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 5,
		})
		handler := mw.Wrap(passthrough("ok"))

		result, err := handler(context.Background(), tc("api"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should isolate breakers per tool", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 1,
			OpenDuration:     1 * time.Second,
		})
		handler := mw.Wrap(failing(errors.New("down")))

		_, _ = handler(context.Background(), tc("tool_a"))
		_, err := handler(context.Background(), tc("tool_a"))
		Expect(err.Error()).To(ContainSubstring("circuit"))

		_, err = handler(context.Background(), tc("tool_b"))
		Expect(err.Error()).To(ContainSubstring("down"))
	})
})
