// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package retrier

import (
	"context"
	"time"
)

type Option struct {
	attempts        int
	backoffDuration time.Duration
	retryIf         func(error) bool // if set, only retry when this returns true
	onRetry         func(int, error) // if set, called before each retry sleep
}

func WithAttempts(attempts int) func(o *Option) {
	return func(o *Option) {
		o.attempts = attempts
	}
}

func WithBackoffDuration(backoffDuration time.Duration) func(o *Option) {
	return func(o *Option) {
		o.backoffDuration = backoffDuration
	}
}

// WithRetryIf sets a predicate that determines whether an error is retryable.
// If the predicate returns false, the error is returned immediately without
// further retries. By default all errors are retried.
func WithRetryIf(fn func(error) bool) func(o *Option) {
	return func(o *Option) {
		o.retryIf = fn
	}
}

// WithOnRetry sets a callback invoked before each retry sleep.
// Useful for logging the retry attempt and error.
func WithOnRetry(fn func(attempt int, err error)) func(o *Option) {
	return func(o *Option) {
		o.onRetry = fn
	}
}

func Retry(ctx context.Context, fn func() error, Options ...func(o *Option)) error {
	opts := &Option{
		attempts:        3,
		backoffDuration: 1 * time.Second,
	}

	for _, o := range Options {
		o(opts)
	}

	var err error
	for i := 0; i < opts.attempts; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		// If a retryIf predicate is set, only retry retryable errors.
		if opts.retryIf != nil && !opts.retryIf(err) {
			return err
		}
		// Don't wait after the last attempt.
		if i < opts.attempts-1 {
			if opts.onRetry != nil {
				opts.onRetry(i+1, err)
			}
			select {
			case <-time.After(opts.backoffDuration):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}
