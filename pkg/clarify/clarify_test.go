/*
Copyright © 2026 StackGen, Inc.
*/

package clarify_test

import (
	"context"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/clarify"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Clarify Store", func() {
	var store *clarify.Store

	BeforeEach(func() {
		store = clarify.NewStore()
	})

	Describe("AskWithID + Respond", func() {
		It("should return a non-empty request ID", func() {
			id, _ := store.AskWithID("What branch?")
			defer store.Cleanup(id)
			Expect(id).NotTo(BeEmpty())
		})

		It("should deliver the user's answer via channel", func() {
			id, ch := store.AskWithID("What branch?")
			defer store.Cleanup(id)

			go func() {
				defer GinkgoRecover()
				time.Sleep(10 * time.Millisecond)
				Expect(store.Respond(id, "main")).To(Succeed())
			}()

			Eventually(ch).Should(Receive(Equal(clarify.Response{Answer: "main"})))
		})

		It("should work when Respond is called before reading the channel", func() {
			id, ch := store.AskWithID("Color?")
			defer store.Cleanup(id)

			Expect(store.Respond(id, "blue")).To(Succeed())

			var resp clarify.Response
			Eventually(ch).Should(Receive(&resp))
			Expect(resp.Answer).To(Equal("blue"))
		})
	})

	Describe("Respond errors", func() {
		It("should return an error for unknown request ID", func() {
			err := store.Respond("does-not-exist", "answer")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should return an error on double respond", func() {
			id, _ := store.AskWithID("Q?")
			defer store.Cleanup(id)

			Expect(store.Respond(id, "first")).To(Succeed())

			err := store.Respond(id, "second")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already answered"))
		})
	})

	Describe("Cleanup", func() {
		It("should remove the pending request so Respond fails", func() {
			id, _ := store.AskWithID("Q?")
			store.Cleanup(id)

			err := store.Respond(id, "answer")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("Context cancellation", func() {
		It("should not receive a response when nobody answers", func() {
			id, ch := store.AskWithID("Slow question?")
			defer store.Cleanup(id)

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			// Channel should not receive anything within 50ms
			Consistently(ch, 50*time.Millisecond).ShouldNot(Receive())
			// Wait for context to actually expire
			<-ctx.Done()
			Expect(ctx.Err()).To(Equal(context.DeadlineExceeded))
		})
	})

	Describe("Concurrent usage", func() {
		It("should handle 50 concurrent ask/respond pairs", func() {
			const n = 50
			var wg sync.WaitGroup
			wg.Add(n)

			for i := 0; i < n; i++ {
				go func(i int) {
					defer GinkgoRecover()
					defer wg.Done()

					id, ch := store.AskWithID("Q?")
					defer store.Cleanup(id)

					go func() {
						defer GinkgoRecover()
						time.Sleep(time.Duration(i) * time.Millisecond)
						Expect(store.Respond(id, "answer")).To(Succeed())
					}()

					Eventually(ch, 5*time.Second).Should(Receive(Equal(clarify.Response{Answer: "answer"})))
				}(i)
			}

			wg.Wait()
		})
	})
})
