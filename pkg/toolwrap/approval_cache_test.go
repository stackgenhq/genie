package toolwrap

import (
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("approvalCache", func() {
	Describe("has", func() {
		It("should return false for an empty cache", func() {
			c := newApprovalCache()
			Expect(c.has("key1")).To(BeFalse())
		})

		It("should return true after adding a key", func() {
			c := newApprovalCache()
			c.add("key1")
			Expect(c.has("key1")).To(BeTrue())
		})

		It("should return false for a key that was never added", func() {
			c := newApprovalCache()
			c.add("key1")
			Expect(c.has("key2")).To(BeFalse())
		})
	})

	Describe("add", func() {
		It("should be idempotent for duplicate keys", func() {
			c := newApprovalCache()
			c.add("key1")
			c.add("key1")
			c.add("key1")

			c.mu.Lock()
			count := len(c.items)
			orderLen := len(c.order)
			c.mu.Unlock()

			Expect(count).To(Equal(1))
			Expect(orderLen).To(Equal(1))
		})

		It("should evict the oldest entry when cache is full", func() {
			c := newApprovalCache()
			for i := 0; i < maxApprovalCacheSize; i++ {
				c.add(fmt.Sprintf("key-%d", i))
			}
			Expect(c.has("key-0")).To(BeTrue())

			c.add("overflow-key")

			Expect(c.has("key-0")).To(BeFalse())
			Expect(c.has("overflow-key")).To(BeTrue())
			Expect(c.has(fmt.Sprintf("key-%d", maxApprovalCacheSize-1))).To(BeTrue())
		})

		It("should evict entries in FIFO order", func() {
			c := newApprovalCache()
			for i := 0; i < maxApprovalCacheSize; i++ {
				c.add(fmt.Sprintf("key-%d", i))
			}

			c.add("overflow-1")
			c.add("overflow-2")
			c.add("overflow-3")

			Expect(c.has("key-0")).To(BeFalse())
			Expect(c.has("key-1")).To(BeFalse())
			Expect(c.has("key-2")).To(BeFalse())
			Expect(c.has("key-3")).To(BeTrue())
			Expect(c.has("overflow-1")).To(BeTrue())
			Expect(c.has("overflow-2")).To(BeTrue())
			Expect(c.has("overflow-3")).To(BeTrue())
		})
	})

	It("should be safe for concurrent access", func() {
		c := newApprovalCache()
		var wg sync.WaitGroup
		const goroutines = 100

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			i := i
			go func() {
				defer wg.Done()
				key := fmt.Sprintf("concurrent-%d", i)
				c.add(key)
				c.has(key)
			}()
		}
		wg.Wait()

		c.mu.Lock()
		count := len(c.items)
		c.mu.Unlock()
		Expect(count).To(Equal(goroutines))
	})
})
