package ttlcache_test

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/ttlcache"
)

type SomeComplexType struct {
	Field1      string
	Field2      int
	CurrentTime time.Time
}

var _ = Describe("TTL Cache", func() {
	Describe("KeepItFresh", func() {
		It("should call the retriever function periodically to refresh the cache", func(ctx context.Context) {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			var retrievalCount atomic.Int32
			retriever := func(ctx context.Context) (SomeComplexType, error) {
				retrievalCount.Add(1)
				return SomeComplexType{
					Field1:      "value",
					Field2:      42,
					CurrentTime: time.Now(),
				}, nil
			}

			item := ttlcache.NewItem(retriever, 100*time.Millisecond)
			By("Starting periodic retrieval every 100ms", func() {
				go func() {
					err := item.KeepItFresh(ctx)
					Expect(err).To(MatchError(context.Canceled))
				}()
			})

			By("Creating a TTL cache item with 100ms TTL", func() {
				// First retrieval should call the retriever
				value1, err := item.GetValue(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(value1.Field1).To(Equal("value"))
				Expect(value1.Field2).To(Equal(42))
				Expect(retrievalCount.Load()).To(Equal(int32(1)))
			})

			By("Waiting for TTL to expire and checking automatic refresh", func() {
				// Wait for TTL to expire and allow automatic refresh to occur
				time.Sleep(250 * time.Millisecond)

				// Retrieval after TTL should have refreshed the value automatically
				value2, err := item.GetValue(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(value2.Field1).To(Equal("value"))
				Expect(value2.Field2).To(Equal(42))
				Expect(retrievalCount.Load()).To(BeNumerically(">", 1)) // Should have been refreshed at least once
			})
		})
	})
	Context("-ve tests", func() {
		It("should return an error when retriever fails", func(ctx context.Context) {
			retriever := func(ctx context.Context) (SomeComplexType, error) {
				return SomeComplexType{}, errors.New("retrieval failed")
			}
			item := ttlcache.NewItem(retriever, 1*time.Second)
			value, err := item.GetValue(ctx)
			Expect(err).To(MatchError(`retrieval failed`))
			Expect(value).To(Equal(SomeComplexType{}))
		})
	})
	It("should cache and refresh values based on TTL", func(ctx context.Context) {
		retrievalCount := 0
		retriever := func(ctx context.Context) (SomeComplexType, error) {
			retrievalCount++
			return SomeComplexType{
				Field1:      "value",
				Field2:      42,
				CurrentTime: time.Now(),
			}, nil
		}
		var firstRetrievalTime time.Time

		item := ttlcache.NewItem(retriever, 100*time.Millisecond)
		By("Creating a TTL cache item with 100ms TTL", func() {
			// First retrieval should call the retriever
			value1, err := item.GetValue(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(value1.Field1).To(Equal("value"))
			Expect(value1.Field2).To(Equal(42))
			firstRetrievalTime = value1.CurrentTime
			Expect(retrievalCount).To(Equal(1))
		})

		By("Retrieving value within TTL should return cached value", func() {
			// Second retrieval within TTL should return cached value
			time.Sleep(50 * time.Millisecond)
			value2, err := item.GetValue(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(value2.CurrentTime).To(Equal(firstRetrievalTime))
			Expect(retrievalCount).To(Equal(1))
		})
		By("Retrieving value after TTL should refresh the value", func() {
			// Wait for TTL to expire
			time.Sleep(100 * time.Millisecond)
			// Third retrieval after TTL should call the retriever again
			value3, err := item.GetValue(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(value3.CurrentTime).NotTo(Equal(firstRetrievalTime))
			Expect(retrievalCount).To(Equal(2))
		})
	})

	It("should prevent concurrent retrievals during cache refresh", func(ctx context.Context) {
		retrievalCount := 0
		retriever := func(ctx context.Context) (SomeComplexType, error) {
			retrievalCount++
			// Simulate slow retrieval
			time.Sleep(50 * time.Millisecond)
			return SomeComplexType{
				Field1:      "value",
				Field2:      42,
				CurrentTime: time.Now(),
			}, nil
		}

		item := ttlcache.NewItem(retriever, 100*time.Millisecond)

		By("Multiple concurrent calls should only trigger one retrieval", func() {
			// Launch multiple goroutines concurrently
			done := make(chan bool, 10)
			for i := 0; i < 10; i++ {
				go func() {
					_, _ = item.GetValue(ctx)
					done <- true
				}()
			}

			// Wait for all goroutines to complete
			for i := 0; i < 10; i++ {
				<-done
			}

			// Only one retrieval should have occurred despite 10 concurrent calls
			Expect(retrievalCount).To(Equal(1))
		})

		By("Concurrent calls after expiry should only trigger one refresh", func() {
			// Wait for cache to expire
			time.Sleep(150 * time.Millisecond)
			retrievalCount = 0

			// Launch multiple goroutines concurrently again
			done := make(chan bool, 10)
			for i := 0; i < 10; i++ {
				go func() {
					_, _ = item.GetValue(ctx)
					done <- true
				}()
			}

			// Wait for all goroutines to complete
			for i := 0; i < 10; i++ {
				<-done
			}

			// Only one refresh should have occurred
			Expect(retrievalCount).To(Equal(1))
		})
		By("Force refresh option should bypass cache", func() {
			// First call to set the cache
			_, err := item.GetValue(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Now force refresh
			value, err := item.GetValueWithOptions(ctx, ttlcache.GetOption{
				ForceRefresh: true,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(value.Field1).To(Equal("value"))
			Expect(retrievalCount).To(Equal(2)) // Retrieval count should have incremented
		})
	})

})
