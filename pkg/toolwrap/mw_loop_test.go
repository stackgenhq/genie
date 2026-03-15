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
	It("should block after 2 identical consecutive calls", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough("ok"))
		handler := mw.Wrap(next)
		tc := &toolwrap.ToolCallContext{ToolName: "search", Args: []byte(`{"q":"same"}`)}

		_, err := handler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())
		_, err = handler(context.Background(), tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))
		Expect(atomic.LoadInt32(count)).To(Equal(int32(1)))
	})

	It("should allow different args for different tools without triggering loop", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		// Alternate between two different tools so same-tool detection doesn't fire.
		for i := 0; i < 6; i++ {
			name := "tool_a"
			if i%2 == 1 {
				name = "tool_b"
			}
			tc := &toolwrap.ToolCallContext{ToolName: name, Args: []byte(fmt.Sprintf(`{"i":%d}`, i))}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("should block after 4 consecutive calls to the same tool with different args", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough("ok"))
		handler := mw.Wrap(next)

		// First 3 calls succeed (different args, same tool).
		for i := 0; i < 3; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "scm_list_repos", Args: []byte(fmt.Sprintf(`{"page":%d}`, i+1))}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// 4th call triggers same-tool loop detection.
		tc := &toolwrap.ToolCallContext{ToolName: "scm_list_repos", Args: []byte(`{"page":4}`)}
		_, err := handler(context.Background(), tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exploration loop detected"))
		Expect(atomic.LoadInt32(count)).To(Equal(int32(3)))
	})

	It("should reset same-tool counter when different tool is called", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		// 3 calls to scm_list_repos, then a different tool, then 3 more.
		for i := 0; i < 3; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "scm_list_repos", Args: []byte(fmt.Sprintf(`{"page":%d}`, i+1))}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// Interject a different tool — resets the same-tool counter.
		_, err := handler(context.Background(), &toolwrap.ToolCallContext{ToolName: "scm_create_pr", Args: []byte(`{"repo":"demo"}`)})
		Expect(err).NotTo(HaveOccurred())

		// 3 more calls to scm_list_repos — should succeed since counter was reset.
		for i := 0; i < 3; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "scm_list_repos", Args: []byte(fmt.Sprintf(`{"page":%d}`, i+10))}
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

		// 3 calls: empty → non-empty (resets streak) → empty.
		// Only 1 consecutive empty at end, well under threshold of 3.
		for i := 0; i < 3; i++ {
			tc := &toolwrap.ToolCallContext{
				ToolName: "memory_search",
				Args:     []byte(fmt.Sprintf(`{"query":"q%d"}`, i)),
			}
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// Counter was reset at call 2 (non-empty), so only 1 consecutive empty at end.
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

		// Alternate tool names to avoid same-tool detection (threshold 4).
		for i := 0; i < 5; i++ {
			name := "run_shell"
			if i%2 == 1 {
				name = "save_file"
			}
			tc := &toolwrap.ToolCallContext{
				ToolName: name,
				Args:     []byte(fmt.Sprintf(`{"cmd":"cmd_%d"}`, i)),
			}
			_, err := handler(ctx, tc)
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(ctx.Err()).NotTo(HaveOccurred())
	})

	It("should trim history after more than 10 distinct calls", func() {
		// Arrange — push 12 distinct calls to trigger the maxHistory=10 trim.
		// Alternate tool names so same-tool detection doesn't fire.
		mw := toolwrap.LoopDetectionMiddleware()
		handler := mw.Wrap(passthrough("ok"))

		for i := 0; i < 12; i++ {
			name := "tool_a"
			if i%2 == 1 {
				name = "tool_b"
			}
			tc := &toolwrap.ToolCallContext{
				ToolName: name,
				Args:     []byte(fmt.Sprintf(`{"i":%d}`, i)),
			}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}

		// After 12 distinct calls, the middleware should still work correctly.
		// Calling a new distinct tool should succeed.
		tc := &toolwrap.ToolCallContext{ToolName: "tool_c", Args: []byte(`{"i":99}`)}
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

	DescribeTable("should exempt Google Drive tools from loop detection when configured",
		func(toolName string) {
			mw := toolwrap.LoopDetectionMiddleware(toolwrap.LoopDetectionConfig{
				ExemptTools: []string{"google_drive_*"},
			})
			next, count := counting(passthrough("file content"))
			handler := mw.Wrap(next)
			tc := &toolwrap.ToolCallContext{ToolName: toolName, Args: []byte(`{"file_id":"abc123"}`)}

			// Same tool + same args called 5 times — should NOT trigger loop
			for i := 0; i < 5; i++ {
				_, err := handler(context.Background(), tc)
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(atomic.LoadInt32(count)).To(Equal(int32(5)))
		},
		Entry("google_drive_read_file", "google_drive_read_file"),
		Entry("google_drive_read_files", "google_drive_read_files"),
		Entry("google_drive_search", "google_drive_search"),
		Entry("google_drive_list_folder", "google_drive_list_folder"),
		Entry("google_drive_get_file", "google_drive_get_file"),
	)

	It("should exempt web_search from loop detection when configured", func() {
		mw := toolwrap.LoopDetectionMiddleware(toolwrap.LoopDetectionConfig{
			ExemptTools: []string{"web_search"},
		})
		next, count := counting(passthrough("search results"))
		handler := mw.Wrap(next)

		// 6 consecutive web_search calls with different args — should NOT trigger loop
		for i := 0; i < 6; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "web_search", Args: []byte(fmt.Sprintf(`{"query":"query_%d"}`, i))}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(atomic.LoadInt32(count)).To(Equal(int32(6)))
	})

	It("should exempt create_agent from identical-args loop detection", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough(`{"output":"done"}`))
		handler := mw.Wrap(next)
		tc := &toolwrap.ToolCallContext{ToolName: "create_agent", Args: []byte(`{"goal":"check pods","tool_names":["run_shell"]}`)}

		// Same create_agent call 5 times — should NOT trigger identical-args loop
		for i := 0; i < 5; i++ {
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(atomic.LoadInt32(count)).To(Equal(int32(5)))
	})

	It("should exempt create_agent from same-tool loop detection", func() {
		mw := toolwrap.LoopDetectionMiddleware()
		next, count := counting(passthrough(`{"output":"done"}`))
		handler := mw.Wrap(next)

		// 6 create_agent calls with different goals — should NOT trigger same-tool loop
		for i := 0; i < 6; i++ {
			tc := &toolwrap.ToolCallContext{
				ToolName: "create_agent",
				Args:     []byte(fmt.Sprintf(`{"goal":"strategy_%d","tool_names":["run_shell"]}`, i)),
			}
			_, err := handler(context.Background(), tc)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(atomic.LoadInt32(count)).To(Equal(int32(6)))
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
