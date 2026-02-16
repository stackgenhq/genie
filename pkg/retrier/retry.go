package retrier

import (
	"context"
	"time"
)

type Option struct {
	attempts        int
	backoffDuration time.Duration
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
		// Don't wait after the last attempt.
		if i < opts.attempts-1 {
			select {
			case <-time.After(opts.backoffDuration):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}
