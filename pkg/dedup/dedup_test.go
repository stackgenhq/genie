// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dedup_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/dedup"
)

func TestDedup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dedup Suite")
}

var _ = Describe("Group", func() {

	Describe("Do", func() {

		It("should execute the function and return its result for a single caller", func() {
			var g dedup.Group[string]

			val, err, shared := g.Do("key1", func() (string, error) {
				return "hello", nil
			})

			Expect(val).To(Equal("hello"))
			Expect(err).NotTo(HaveOccurred())
			Expect(shared).To(BeFalse())
		})

		It("should execute the function exactly once for duplicate concurrent callers", func() {
			var g dedup.Group[int]
			var calls atomic.Int32

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			results := make([]int, n)
			errs := make([]error, n)
			shareds := make([]bool, n)

			for i := 0; i < n; i++ {
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					results[idx], errs[idx], shareds[idx] = g.Do("same-key", func() (int, error) {
						calls.Add(1)
						time.Sleep(50 * time.Millisecond)
						return 42, nil
					})
				}(i)
			}

			wg.Wait()

			Expect(calls.Load()).To(Equal(int32(1)), "fn should be called exactly once")

			sharedCount := 0
			for i := 0; i < n; i++ {
				Expect(results[i]).To(Equal(42))
				Expect(errs[i]).NotTo(HaveOccurred())
				if shareds[i] {
					sharedCount++
				}
			}

			// singleflight may mark all callers as shared.
			Expect(sharedCount).To(BeNumerically(">=", n-1))
		})

		It("should propagate errors to all callers", func() {
			var g dedup.Group[string]
			boom := errors.New("boom")

			val, err, shared := g.Do("err-key", func() (string, error) {
				return "", boom
			})

			Expect(val).To(BeEmpty())
			Expect(err).To(MatchError(boom))
			Expect(shared).To(BeFalse())
		})

		It("should allow key reuse after completion", func() {
			var g dedup.Group[int]
			var calls atomic.Int32

			val1, _, _ := g.Do("reuse-key", func() (int, error) {
				calls.Add(1)
				return 1, nil
			})

			val2, _, _ := g.Do("reuse-key", func() (int, error) {
				calls.Add(1)
				return 2, nil
			})

			Expect(calls.Load()).To(Equal(int32(2)), "sequential calls should each execute")
			Expect(val1).To(Equal(1))
			Expect(val2).To(Equal(2))
		})

		It("should run different keys concurrently", func() {
			var g dedup.Group[string]
			var running atomic.Int32
			var maxConcurrent atomic.Int32

			const n = 5
			var wg sync.WaitGroup
			wg.Add(n)

			for i := 0; i < n; i++ {
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					g.Do("unique-key-"+string(rune('A'+idx)), func() (string, error) {
						cur := running.Add(1)
						for {
							old := maxConcurrent.Load()
							if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
								break
							}
						}
						time.Sleep(50 * time.Millisecond)
						running.Add(-1)
						return "done", nil
					})
				}(i)
			}

			wg.Wait()

			// On slow CI this might be low, but we expect some parallelism.
			Expect(maxConcurrent.Load()).To(BeNumerically(">=", 2),
				"different keys should run in parallel")
		})
	})
})
