// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("RetryMiddleware", func() {
	It("should succeed immediately on first success", func() {
		mw := toolwrap.RetryMiddleware(toolwrap.RetryConfig{MaxAttempts: 3})
		next, count := counting(passthrough("ok"))
		handler := mw.Wrap(next)

		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
		Expect(atomic.LoadInt32(count)).To(Equal(int32(1)))
	})

	It("should retry on failure and eventually succeed", func() {
		callNum := 0
		mw := toolwrap.RetryMiddleware(toolwrap.RetryConfig{
			MaxAttempts:    3,
			InitialBackoff: 1 * time.Millisecond,
		})
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			callNum++
			if callNum < 3 {
				return nil, errors.New("transient")
			}
			return "recovered", nil
		})

		result, err := handler(context.Background(), tc("test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("recovered"))
		Expect(callNum).To(Equal(3))
	})

	It("should fail after exhausting retries", func() {
		mw := toolwrap.RetryMiddleware(toolwrap.RetryConfig{
			MaxAttempts:    2,
			InitialBackoff: 1 * time.Millisecond,
		})
		handler := mw.Wrap(failing(errors.New("permanent")))

		_, err := handler(context.Background(), tc("test"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed after 2 attempts"))
	})

	It("should NOT retry non-retryable errors", func() {
		callNum := 0
		mw := toolwrap.RetryMiddleware(toolwrap.RetryConfig{
			MaxAttempts:    3,
			InitialBackoff: 1 * time.Millisecond,
			Retryable:      func(err error) bool { return false },
		})
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			callNum++
			return nil, errors.New("fatal")
		})

		_, err := handler(context.Background(), tc("test"))
		Expect(err).To(HaveOccurred())
		Expect(callNum).To(Equal(1))
	})
})
