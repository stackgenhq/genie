package toolwrap_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ContextEnrichMiddleware", func() {
	It("should inject ThreadID and RunID into context", func() {
		mw := toolwrap.ContextEnrichMiddleware(nil, "t1", "r1", nil)
		var capturedCtx context.Context
		handler := mw.Wrap(func(ctx context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			capturedCtx = ctx
			return "ok", nil
		})

		_, _ = handler(context.Background(), tc("test"))
		Expect(agui.ThreadIDFromContext(capturedCtx)).To(Equal("t1"))
		Expect(agui.RunIDFromContext(capturedCtx)).To(Equal("r1"))
	})

	It("should not overwrite existing context values", func() {
		mw := toolwrap.ContextEnrichMiddleware(nil, "struct-t", "struct-r", nil)
		var capturedCtx context.Context
		handler := mw.Wrap(func(ctx context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			capturedCtx = ctx
			return "ok", nil
		})

		parentCtx := agui.WithThreadID(context.Background(), "existing-t")
		parentCtx = agui.WithRunID(parentCtx, "existing-r")

		_, _ = handler(parentCtx, tc("test"))
		Expect(agui.ThreadIDFromContext(capturedCtx)).To(Equal("existing-t"))
		Expect(agui.RunIDFromContext(capturedCtx)).To(Equal("existing-r"))
	})
})
