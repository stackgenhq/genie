package toolwrap_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("CompositeMiddleware", func() {
	It("should execute middlewares in order", func() {
		var order []string
		mkMW := func(name string) toolwrap.MiddlewareFunc {
			return func(next toolwrap.Handler) toolwrap.Handler {
				return func(ctx context.Context, tc *toolwrap.ToolCallContext) (any, error) {
					order = append(order, name+":before")
					result, err := next(ctx, tc)
					order = append(order, name+":after")
					return result, err
				}
			}
		}

		chain := toolwrap.CompositeMiddleware{mkMW("A"), mkMW("B"), mkMW("C")}
		handler := chain.Wrap(passthrough("ok"))
		handler(context.Background(), tc("test")) //nolint:errcheck

		Expect(order).To(Equal([]string{
			"A:before", "B:before", "C:before",
			"C:after", "B:after", "A:after",
		}))
	})
})
