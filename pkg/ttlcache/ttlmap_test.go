package ttlcache_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/appcd-dev/genie/pkg/ttlcache"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TTLMap", func() {

	Describe("basic operations", func() {
		It("should store and retrieve a value", func() {
			m := ttlcache.NewTTLMap[string](16, time.Minute)
			m.Set("greeting", "hello")

			val, ok := m.Get("greeting")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("hello"))
		})

		It("should return false for missing keys", func() {
			m := ttlcache.NewTTLMap[int](16, time.Minute)

			_, ok := m.Get("nonexistent")
			Expect(ok).To(BeFalse())
		})

		It("should overwrite existing keys", func() {
			m := ttlcache.NewTTLMap[string](16, time.Minute)
			m.Set("key", "v1")
			m.Set("key", "v2")

			val, ok := m.Get("key")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("v2"))
			Expect(m.Len()).To(Equal(1))
		})

		It("should delete a key", func() {
			m := ttlcache.NewTTLMap[string](16, time.Minute)
			m.Set("key", "value")
			m.Delete("key")

			_, ok := m.Get("key")
			Expect(ok).To(BeFalse())
			Expect(m.Len()).To(Equal(0))
		})

		It("should delete non-existent keys without panic", func() {
			m := ttlcache.NewTTLMap[string](16, time.Minute)
			Expect(func() { m.Delete("nope") }).NotTo(Panic())
		})

		It("should report correct length", func() {
			m := ttlcache.NewTTLMap[int](16, time.Minute)
			Expect(m.Len()).To(Equal(0))

			m.Set("a", 1)
			m.Set("b", 2)
			m.Set("c", 3)
			Expect(m.Len()).To(Equal(3))

			m.Delete("b")
			Expect(m.Len()).To(Equal(2))
		})
	})

	Describe("TTL expiry", func() {
		It("should expire entries after the TTL", func() {
			m := ttlcache.NewTTLMap[string](16, 50*time.Millisecond)
			m.Set("ephemeral", "gone soon")

			val, ok := m.Get("ephemeral")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("gone soon"))

			time.Sleep(80 * time.Millisecond)

			_, ok = m.Get("ephemeral")
			Expect(ok).To(BeFalse(), "entry should have expired")
		})

		It("should support custom per-entry TTL via SetWithTTL", func() {
			m := ttlcache.NewTTLMap[string](16, time.Minute)
			m.SetWithTTL("short", "bye", 50*time.Millisecond)
			m.Set("long", "still here")

			time.Sleep(80 * time.Millisecond)

			_, ok := m.Get("short")
			Expect(ok).To(BeFalse(), "short-TTL entry should have expired")

			val, ok := m.Get("long")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("still here"))
		})

		It("should reset TTL when value is overwritten", func() {
			m := ttlcache.NewTTLMap[string](16, 80*time.Millisecond)
			m.Set("key", "v1")

			time.Sleep(50 * time.Millisecond)
			// Overwrite before expiry — should reset the TTL.
			m.Set("key", "v2")

			time.Sleep(50 * time.Millisecond)
			// 100ms total since first Set, but only 50ms since the overwrite.
			val, ok := m.Get("key")
			Expect(ok).To(BeTrue(), "TTL should have been reset by overwrite")
			Expect(val).To(Equal("v2"))
		})

		It("should lazily clean up expired entries on Get", func() {
			m := ttlcache.NewTTLMap[string](16, 50*time.Millisecond)
			m.Set("key", "value")
			Expect(m.Len()).To(Equal(1))

			time.Sleep(80 * time.Millisecond)
			m.Get("key") // triggers lazy cleanup
			Expect(m.Len()).To(Equal(0))
		})
	})

	Describe("LRU eviction", func() {
		It("should evict the least recently used entry when at capacity", func() {
			m := ttlcache.NewTTLMap[string](3, time.Minute)
			m.Set("a", "1")
			m.Set("b", "2")
			m.Set("c", "3")

			// Adding a 4th entry should evict "a" (oldest).
			m.Set("d", "4")

			_, ok := m.Get("a")
			Expect(ok).To(BeFalse(), "LRU entry 'a' should have been evicted")

			val, ok := m.Get("d")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("4"))
			Expect(m.Len()).To(Equal(3))
		})

		It("should promote accessed entries so they are not evicted", func() {
			m := ttlcache.NewTTLMap[string](3, time.Minute)
			m.Set("a", "1")
			m.Set("b", "2")
			m.Set("c", "3")

			// Access "a" to promote it to MRU.
			m.Get("a")

			// Now insert "d" — should evict "b" (now the LRU).
			m.Set("d", "4")

			_, ok := m.Get("b")
			Expect(ok).To(BeFalse(), "LRU entry 'b' should have been evicted")

			val, ok := m.Get("a")
			Expect(ok).To(BeTrue(), "'a' was accessed recently so should still be present")
			Expect(val).To(Equal("1"))
		})

		It("should handle maxSize of 1", func() {
			m := ttlcache.NewTTLMap[string](1, time.Minute)
			m.Set("a", "1")
			m.Set("b", "2")

			_, ok := m.Get("a")
			Expect(ok).To(BeFalse())

			val, ok := m.Get("b")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("2"))
		})

		It("should default to 128 when maxSize <= 0", func() {
			m := ttlcache.NewTTLMap[int](0, time.Minute)
			for i := 0; i < 128; i++ {
				m.Set(fmt.Sprintf("k%d", i), i)
			}
			Expect(m.Len()).To(Equal(128))

			// 129th entry should evict.
			m.Set("overflow", 999)
			Expect(m.Len()).To(Equal(128))
		})
	})

	Describe("concurrency safety", func() {
		It("should handle concurrent reads and writes safely", func() {
			m := ttlcache.NewTTLMap[int](128, time.Minute)
			var wg sync.WaitGroup

			// Concurrent writers.
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					m.Set(fmt.Sprintf("key-%d", n), n)
				}(i)
			}

			// Concurrent readers.
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					m.Get(fmt.Sprintf("key-%d", n))
				}(i)
			}

			// Concurrent deleters.
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					m.Delete(fmt.Sprintf("key-%d", n))
				}(i)
			}

			wg.Wait()
			// No panics or data races = success.
		})
	})
})

// ---------- Benchmarks ----------

func BenchmarkTTLMapSet(b *testing.B) {
	m := ttlcache.NewTTLMap[int](4096, time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Set(fmt.Sprintf("key-%d", i), i)
	}
}

func BenchmarkTTLMapGet(b *testing.B) {
	m := ttlcache.NewTTLMap[int](4096, time.Minute)
	for i := 0; i < 4096; i++ {
		m.Set(fmt.Sprintf("key-%d", i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Get(fmt.Sprintf("key-%d", i%4096))
	}
}

func BenchmarkTTLMapSetEviction(b *testing.B) {
	m := ttlcache.NewTTLMap[int](128, time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Set(fmt.Sprintf("key-%d", i), i)
	}
}

func BenchmarkTTLMapConcurrent(b *testing.B) {
	m := ttlcache.NewTTLMap[int](4096, time.Minute)
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%4096)
			if i%2 == 0 {
				m.Set(key, i)
			} else {
				m.Get(key)
			}
			i++
		}
	})
}
