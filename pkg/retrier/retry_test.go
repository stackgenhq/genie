package retrier_test

import (
	"context"
	"errors"
	"time"

	"github.com/appcd-dev/genie/pkg/retrier"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

})
