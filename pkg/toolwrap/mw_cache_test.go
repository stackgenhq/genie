package toolwrap_test

import (
	"context"
	"errors"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("SemanticCacheMiddleware", func() {
	It("should cache results for configured tools", func() {
		mw := toolwrap.SemanticCacheMiddleware(map[string][]string{
			"create_task": {"name"},
		})
		next, count := counting(passthrough("created"))
		handler := mw.Wrap(next)
		tc := &toolwrap.ToolCallContext{ToolName: "create_task", Args: []byte(`{"name":"daily"}`)}

		r1, _ := handler(context.Background(), tc)
		r2, _ := handler(context.Background(), tc)
		Expect(r1).To(Equal("created"))
		Expect(r2).To(Equal("created"))
		Expect(atomic.LoadInt32(count)).To(Equal(int32(1)))
	})

	It("should NOT cache unconfigured tools", func() {
		mw := toolwrap.SemanticCacheMiddleware(nil)
		next, count := counting(passthrough("data"))
		handler := mw.Wrap(next)

		handler(context.Background(), tc("read_file")) //nolint:errcheck
		handler(context.Background(), tc("read_file")) //nolint:errcheck
		Expect(atomic.LoadInt32(count)).To(Equal(int32(2)))
	})

	It("should NOT cache failed calls", func() {
		mw := toolwrap.SemanticCacheMiddleware(map[string][]string{
			"create_task": {"name"},
		})
		callNum := 0
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			callNum++
			if callNum == 1 {
				return nil, errors.New("fail")
			}
			return "ok", nil
		})
		tc := &toolwrap.ToolCallContext{ToolName: "create_task", Args: []byte(`{"name":"daily"}`)}

		_, err := handler(context.Background(), tc)
		Expect(err).To(HaveOccurred())
		result, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})
})
