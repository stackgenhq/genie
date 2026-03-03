package toolwrap_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("EmptyResultsMiddleware", func() {
	emptyJSON := func() any {
		return json.RawMessage(`{"results":[],"count":0}`)
	}

	nonEmptyJSON := func() any {
		return json.RawMessage(`{"results":[{"content":"data"}],"count":1}`)
	}

	It("should cancel context after 3 consecutive empty memory_search results", func() {
		mw := toolwrap.EmptyResultsMiddleware()
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return emptyJSON(), nil
		})

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		tc := &toolwrap.ToolCallContext{ToolName: "memory_search", Args: []byte(`{"query":"test"}`)}

		for i := 0; i < 3; i++ {
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// Context should be cancelled after 3 empty results.
		Expect(ctx.Err()).To(HaveOccurred())
		Expect(context.Cause(ctx).Error()).To(ContainSubstring("empty results"))
	})

	It("should reset counter on non-empty result", func() {
		mw := toolwrap.EmptyResultsMiddleware()
		callNum := 0
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			callNum++
			if callNum == 2 {
				return nonEmptyJSON(), nil
			}
			return emptyJSON(), nil
		})

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		tc := &toolwrap.ToolCallContext{ToolName: "memory_search", Args: []byte(`{"query":"test"}`)}

		// 1st empty
		_, err := handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		// 2nd non-empty — resets counter
		_, err = handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		// 3rd empty (counter back to 1)
		_, err = handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		// 4th empty (counter at 2)
		_, err = handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		// Context should NOT be cancelled (only 2 consecutive, not 3).
		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should track different tools independently", func() {
		mw := toolwrap.EmptyResultsMiddleware()
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return emptyJSON(), nil
		})

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		// 2 empty memory_search calls
		for i := 0; i < 2; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "memory_search", Args: []byte(`{"query":"test"}`)}
			_, _ = handler(ctx, tc)
		}

		// 2 empty graph_query calls
		for i := 0; i < 2; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "graph_query", Args: []byte(`{"entity_id":"x"}`)}
			_, _ = handler(ctx, tc)
		}

		// No single tool hit 3 consecutive empties.
		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should not track non-search tools", func() {
		mw := toolwrap.EmptyResultsMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		tc := &toolwrap.ToolCallContext{ToolName: "run_shell", Args: []byte(`{}`)}
		for i := 0; i < 5; i++ {
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should not cancel when no CancelCauseFunc is in context", func() {
		mw := toolwrap.EmptyResultsMiddleware()
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return emptyJSON(), nil
		})

		// No WithCancelCause — simulates parent agent context.
		ctx := context.Background()
		tc := &toolwrap.ToolCallContext{ToolName: "memory_search", Args: []byte(`{"query":"test"}`)}

		for i := 0; i < 5; i++ {
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}
		// Should not panic or crash — gracefully does nothing.
	})
})
