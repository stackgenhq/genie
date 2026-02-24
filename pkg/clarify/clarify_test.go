/*
Copyright © 2026 StackGen, Inc.
*/

package clarify_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/clarify"
	geniedb "github.com/stackgenhq/genie/pkg/db"
)

func newTestStore() clarify.Store {
	tmpDir := GinkgoT().TempDir()
	dbPath := filepath.Join(tmpDir, "clarify_test.db")
	gormDB, err := geniedb.Open(dbPath)
	Expect(err).NotTo(HaveOccurred())
	Expect(geniedb.AutoMigrate(gormDB)).To(Succeed())
	DeferCleanup(func() {
		_ = geniedb.Close(gormDB)
		_ = os.RemoveAll(tmpDir)
	})
	return clarify.NewStore(gormDB)
}

var _ = Describe("Clarify Store", func() {
	var store clarify.Store

	BeforeEach(func() {
		store = newTestStore()
	})

	Describe("Ask + Respond", func() {
		It("should return a non-empty request ID", func() {
			id, _, err := store.Ask(context.Background(), "What branch?", "", "")
			Expect(err).NotTo(HaveOccurred())
			defer store.Cleanup(id)
			Expect(id).NotTo(BeEmpty())
		})

		It("should deliver the user's answer via WaitForResponse", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			id, ch, err := store.Ask(ctx, "What branch?", "", "")
			Expect(err).NotTo(HaveOccurred())
			defer store.Cleanup(id)

			// Respond in a goroutine — capture errors via channel
			errCh := make(chan error, 1)
			go func() {
				time.Sleep(50 * time.Millisecond)
				errCh <- store.Respond(id, "main")
			}()

			resp, waitErr := store.WaitForResponse(ctx, id, ch)
			Expect(waitErr).NotTo(HaveOccurred())
			Expect(resp.Answer).To(Equal("main"))

			// Also verify Respond didn't error
			Eventually(errCh).Should(Receive(BeNil()))
		})

		It("should work when Respond is called before WaitForResponse", func() {
			ctx := context.Background()
			id, ch, err := store.Ask(ctx, "Color?", "", "")
			Expect(err).NotTo(HaveOccurred())
			defer store.Cleanup(id)

			Expect(store.Respond(id, "blue")).To(Succeed())

			waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			resp, err := store.WaitForResponse(waitCtx, id, ch)
			Expect(err).NotTo(HaveOccurred())
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
			ctx := context.Background()
			id, _, err := store.Ask(ctx, "Q?", "", "")
			Expect(err).NotTo(HaveOccurred())
			defer store.Cleanup(id)

			Expect(store.Respond(id, "first")).To(Succeed())

			err = store.Respond(id, "second")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already answered"))
		})
	})

	Describe("Cleanup", func() {
		It("should remove the in-process waiter", func() {
			ctx := context.Background()
			id, _, err := store.Ask(ctx, "Q?", "", "")
			Expect(err).NotTo(HaveOccurred())
			store.Cleanup(id)
		})
	})

	Describe("WaitForResponse timeout", func() {
		It("should return error when context expires", func() {
			ctx := context.Background()
			id, ch, err := store.Ask(ctx, "Slow question?", "", "")
			Expect(err).NotTo(HaveOccurred())
			defer store.Cleanup(id)

			waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()

			_, err = store.WaitForResponse(waitCtx, id, ch)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("did not answer"))
		})
	})

	Describe("Concurrent usage", func() {
		It("should handle 5 concurrent ask/respond pairs", func() {
			const n = 5
			var wg sync.WaitGroup
			wg.Add(n)
			ctx := context.Background()

			for i := 0; i < n; i++ {
				go func(i int) {
					defer GinkgoRecover()
					defer wg.Done()

					askCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()

					id, ch, err := store.Ask(askCtx, "Q?", "", "")
					Expect(err).NotTo(HaveOccurred())
					defer store.Cleanup(id)

					// Respond in a goroutine
					errCh := make(chan error, 1)
					go func() {
						time.Sleep(time.Duration(i*10) * time.Millisecond)
						errCh <- store.Respond(id, "answer")
					}()

					resp, err := store.WaitForResponse(askCtx, id, ch)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp.Answer).To(Equal("answer"))

					// Verify Respond succeeded
					Eventually(errCh, 10*time.Second).Should(Receive(BeNil()))
				}(i)
			}

			wg.Wait()
		})
	})

	Describe("FindPendingByQuestion", func() {
		It("should find a pending question by text", func() {
			ctx := context.Background()
			id, _, err := store.Ask(ctx, "What env?", "", "")
			Expect(err).NotTo(HaveOccurred())
			defer store.Cleanup(id)

			foundID, found := store.FindPendingByQuestion(ctx, "What env?")
			Expect(found).To(BeTrue())
			Expect(foundID).To(Equal(id))
		})

		It("should not find an answered question", func() {
			ctx := context.Background()
			id, _, err := store.Ask(ctx, "What env?", "", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Respond(id, "prod")).To(Succeed())

			_, found := store.FindPendingByQuestion(ctx, "What env?")
			Expect(found).To(BeFalse())
		})
	})

	Describe("RecoverPending", func() {
		It("should recover recent pending questions", func() {
			ctx := context.Background()
			_, _, err := store.Ask(ctx, "Pending Q?", "", "")
			Expect(err).NotTo(HaveOccurred())

			result, err := store.RecoverPending(ctx, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Recovered).To(Equal(1))
			Expect(result.Expired).To(Equal(0))
		})
	})
})
