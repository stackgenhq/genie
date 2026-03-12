// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	It("should cancel context when CancelCauseFunc is set", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		tc := &toolwrap.ToolCallContext{ToolName: "run_shell", Args: []byte(`{"cmd":"kubectl get pods"}`)}

		_, err := handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(ctx, tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))

		// Context should be cancelled by the middleware.
		Expect(ctx.Err()).To(HaveOccurred())
		Expect(context.Cause(ctx).Error()).To(ContainSubstring("loop detected"))
	})

	It("should not cancel context when no CancelCauseFunc is set (backward compat)", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		ctx := context.Background()
		tc := &toolwrap.ToolCallContext{ToolName: "run_shell", Args: []byte(`{"cmd":"ls"}`)}

		_, err := handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(ctx, tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))

		// Context should NOT be cancelled — no cancel function available.
		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should cancel context after 3 consecutive empty retrieval results", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return json.RawMessage(`{"results":[],"count":0}`), nil
		})

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		// 3 different queries, all returning empty — result loop
		for i := 0; i < 3; i++ {
			tc := &toolwrap.ToolCallContext{
				ToolName: "memory_search",
				Args:     []byte(fmt.Sprintf(`{"query":"attempt_%d"}`, i)),
			}
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(ctx.Err()).To(HaveOccurred())
		Expect(context.Cause(ctx).Error()).To(ContainSubstring("result loop"))
	})

	It("should reset empty streak on non-empty retrieval result", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		callNum := 0
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			callNum++
			if callNum == 2 {
				return json.RawMessage(`{"results":[{"data":"x"}],"count":1}`), nil
			}
			return json.RawMessage(`{"results":[],"count":0}`), nil
		})

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		for i := 0; i < 4; i++ {
			tc := &toolwrap.ToolCallContext{
				ToolName: "memory_search",
				Args:     []byte(fmt.Sprintf(`{"query":"q%d"}`, i)),
			}
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// Counter was reset at call 2 (non-empty), so only 2 consecutive empties at end.
		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should not track empty results for non-retrieval tools", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return json.RawMessage(`{"results":[],"count":0}`), nil
		})

		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		ctx = toolwrap.WithCancelCause(ctx, cancel)

		for i := 0; i < 5; i++ {
			tc := &toolwrap.ToolCallContext{
				ToolName: "run_shell",
				Args:     []byte(fmt.Sprintf(`{"cmd":"cmd_%d"}`, i)),
			}
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should trim history after more than 10 distinct calls", func() {
		// Arrange — push 12 distinct calls to trigger the maxHistory=10 trim.
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		for i := 0; i < 12; i++ {
			tc := &toolwrap.ToolCallContext{
				ToolName: "run_shell",
				Args:     []byte(fmt.Sprintf(`{"i":%d}`, i)),
			}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// After 12 distinct calls, the middleware should still work correctly.
		// Calling a new distinct tool should succeed.
		tc := &toolwrap.ToolCallContext{ToolName: "run_shell", Args: []byte(`{"i":99}`)}
		_, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should exempt read_notes from loop detection", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough("notes content"))
		handler := mw.Wrap(next)
		tc := &toolwrap.ToolCallContext{ToolName: "read_notes", Args: []byte(`{}`)}

		// Call the same tool with same args multiple times — should NOT trigger loop
		for i := 0; i < 5; i++ {
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(atomic.LoadInt32(count)).To(Equal(int32(5)))
	})

	It("should exempt note from loop detection", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough("saved"))
		handler := mw.Wrap(next)
		tc := &toolwrap.ToolCallContext{ToolName: "note", Args: []byte(`{"text":"remember this"}`)}

		// Call the same tool with same args multiple times — should NOT trigger loop
		for i := 0; i < 5; i++ {
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(atomic.LoadInt32(count)).To(Equal(int32(5)))
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

var _ = Describe("IsRetrievalTool", func() {
	DescribeTable("classifies tools correctly",
		func(name string, expected bool) {
			Expect(toolwrap.IsRetrievalTool(name)).To(Equal(expected))
		},
		Entry("memory_search is retrieval", "memory_search", true),
		Entry("graph_query is retrieval", "graph_query", true),
		Entry("graph_store is NOT retrieval", "graph_store", false),
		Entry("run_shell is NOT retrieval", "run_shell", false),
		Entry("read_file is NOT retrieval", "read_file", false),
		Entry("empty string is NOT retrieval", "", false),
	)
})
