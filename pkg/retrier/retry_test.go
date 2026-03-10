// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package retrier_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/retrier"
)

var _ = Describe("Retrier", func() {
	var (
		callCount int
	)
	BeforeEach(func() {
		callCount = 0
	})
	Describe("Retry", func() {
		It("should not error when func does not return err", func(ctx context.Context) {
			err := retrier.Retry(ctx, func() error {
				callCount++
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(Equal(1))
		})
		It("should error when func returns err", func(ctx context.Context) {
			err := retrier.Retry(ctx, func() error {
				callCount++
				return errors.New("something is wrong")
			},
				retrier.WithAttempts(5),
				retrier.WithBackoffDuration(1*time.Nanosecond),
			)
			Expect(err).To(MatchError(`something is wrong`))
			Expect(callCount).To(Equal(5))
		})
		It("should return ctx.Err() when ctx is done", func(ctx context.Context) {
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			err := retrier.Retry(ctx, func() error {
				callCount++
				return errors.New("something is wrong")
			})
			Expect(err).To(MatchError(`context canceled`))
			Expect(callCount).To(Equal(1))
		})
		It("should return nil when func returns nil on 2nd attempt", func(ctx context.Context) {
			err := retrier.Retry(ctx, func() error {
				callCount++
				if callCount == 2 {
					return nil
				}
				return errors.New("something is wrong")
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(Equal(2))
		})
	})

	Describe("WithRetryIf", func() {
		It("should stop retrying when predicate returns false", func(ctx context.Context) {
			permanent := errors.New("permanent failure")
			err := retrier.Retry(ctx, func() error {
				callCount++
				return permanent
			},
				retrier.WithAttempts(5),
				retrier.WithBackoffDuration(1*time.Nanosecond),
				retrier.WithRetryIf(func(e error) bool {
					return false // never retry
				}),
			)
			Expect(err).To(MatchError("permanent failure"))
			Expect(callCount).To(Equal(1)) // only called once
		})

		It("should continue retrying when predicate returns true", func(ctx context.Context) {
			transient := errors.New("transient error")
			err := retrier.Retry(ctx, func() error {
				callCount++
				return transient
			},
				retrier.WithAttempts(3),
				retrier.WithBackoffDuration(1*time.Nanosecond),
				retrier.WithRetryIf(func(e error) bool {
					return true // always retry
				}),
			)
			Expect(err).To(MatchError("transient error"))
			Expect(callCount).To(Equal(3)) // exhausts all attempts
		})

		It("should stop on first non-retryable error after retryable ones", func(ctx context.Context) {
			permanent := errors.New("permanent")
			transient := errors.New("transient")

			err := retrier.Retry(ctx, func() error {
				callCount++
				if callCount <= 2 {
					return transient
				}
				return permanent
			},
				retrier.WithAttempts(5),
				retrier.WithBackoffDuration(1*time.Nanosecond),
				retrier.WithRetryIf(func(e error) bool {
					return errors.Is(e, transient)
				}),
			)
			Expect(err).To(MatchError("permanent"))
			Expect(callCount).To(Equal(3)) // 2 retryable + 1 permanent
		})

		It("should succeed even with retryIf when func succeeds", func(ctx context.Context) {
			err := retrier.Retry(ctx, func() error {
				callCount++
				if callCount < 2 {
					return errors.New("transient")
				}
				return nil
			},
				retrier.WithAttempts(5),
				retrier.WithBackoffDuration(1*time.Nanosecond),
				retrier.WithRetryIf(func(e error) bool {
					return true
				}),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(Equal(2))
		})
	})

	Describe("WithOnRetry", func() {
		It("should call onRetry callback before each retry sleep", func(ctx context.Context) {
			var retryAttempts []int
			var retryErrors []error

			err := retrier.Retry(ctx, func() error {
				callCount++
				return errors.New("fail")
			},
				retrier.WithAttempts(4),
				retrier.WithBackoffDuration(1*time.Nanosecond),
				retrier.WithOnRetry(func(attempt int, e error) {
					retryAttempts = append(retryAttempts, attempt)
					retryErrors = append(retryErrors, e)
				}),
			)
			Expect(err).To(HaveOccurred())
			Expect(callCount).To(Equal(4))
			// onRetry is called before each retry sleep (not after the last attempt).
			Expect(retryAttempts).To(Equal([]int{1, 2, 3}))
			Expect(retryErrors).To(HaveLen(3))
		})

		It("should not call onRetry when func succeeds on first attempt", func(ctx context.Context) {
			called := false
			err := retrier.Retry(ctx, func() error {
				return nil
			},
				retrier.WithOnRetry(func(attempt int, e error) {
					called = true
				}),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(called).To(BeFalse())
		})
	})

	Describe("WithRetryIf and WithOnRetry combined", func() {
		It("should call onRetry only for retryable errors", func(ctx context.Context) {
			var retryAttempts []int
			permanent := errors.New("permanent")

			err := retrier.Retry(ctx, func() error {
				callCount++
				if callCount <= 2 {
					return errors.New("transient")
				}
				return permanent
			},
				retrier.WithAttempts(10),
				retrier.WithBackoffDuration(1*time.Nanosecond),
				retrier.WithRetryIf(func(e error) bool {
					return e.Error() == "transient"
				}),
				retrier.WithOnRetry(func(attempt int, e error) {
					retryAttempts = append(retryAttempts, attempt)
				}),
			)
			Expect(err).To(MatchError("permanent"))
			Expect(callCount).To(Equal(3))
			// onRetry called for attempts 1 and 2 (the transient ones)
			Expect(retryAttempts).To(Equal([]int{1, 2}))
		})
	})

	Describe("defaults", func() {
		It("should default to 3 attempts", func(ctx context.Context) {
			err := retrier.Retry(ctx, func() error {
				callCount++
				return errors.New("fail")
			},
				retrier.WithBackoffDuration(1*time.Nanosecond),
			)
			Expect(err).To(HaveOccurred())
			Expect(callCount).To(Equal(3))
		})
	})
})
