package clarify_test

import (
	"context"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/clarify"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewTool", func() {
	It("should create the ask_clarifying_question tool", func() {
		store := newTestStore()
		emitter := func(_ context.Context, _ clarify.ClarificationEvent) error { return nil }
		t := clarify.NewTool(store, emitter)
		Expect(t.Declaration().Name).To(Equal("ask_clarifying_question"))
	})
})

var _ = Describe("Do", func() {
	It("should return error for empty question", func(ctx context.Context) {
		store := newTestStore()
		emitter := func(_ context.Context, _ clarify.ClarificationEvent) error { return nil }
		ct := clarify.NewTool(store, emitter)

		_, err := ct.Call(ctx, []byte(`{"question":""}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("question is required"))
	})

	It("should emit event and return user answer", func() {
		store := newTestStore()
		var mu sync.Mutex
		var emittedEvent clarify.ClarificationEvent
		emitter := func(_ context.Context, evt clarify.ClarificationEvent) error {
			mu.Lock()
			emittedEvent = evt
			mu.Unlock()
			return nil
		}

		ct := clarify.NewTool(store, emitter)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Start the question in a goroutine
		type callResult struct {
			output string
			err    error
		}
		resultCh := make(chan callResult, 1)
		go func() {
			result, err := ct.Call(ctx, []byte(`{"question":"What is your name?","context":"Need to personalize"}`))
			if err != nil {
				resultCh <- callResult{err: err}
				return
			}
			resultCh <- callResult{output: result.(string)}
		}()

		// Wait for the question to be registered, then answer it
		Eventually(func() string {
			mu.Lock()
			defer mu.Unlock()
			return emittedEvent.RequestID
		}, 5*time.Second).ShouldNot(BeEmpty())
		mu.Lock()
		reqID := emittedEvent.RequestID
		mu.Unlock()
		err := store.Respond(reqID, "Alice")
		Expect(err).NotTo(HaveOccurred())

		// Wait for the tool call to complete
		var result callResult
		Eventually(resultCh, 10*time.Second).Should(Receive(&result))
		Expect(result.err).NotTo(HaveOccurred())
		Expect(result.output).To(ContainSubstring("Alice"))
	})

	It("should deduplicate same question asked twice", func() {
		store := newTestStore()
		emitter := func(_ context.Context, _ clarify.ClarificationEvent) error { return nil }
		ct := clarify.NewTool(store, emitter)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Ask the first question in background
		go func() {
			ct.Call(ctx, []byte(`{"question":"What is your name?"}`))
		}()
		// Wait for it to register
		time.Sleep(100 * time.Millisecond)

		// Ask the same question again — should be deduped
		result, err := ct.Call(ctx, []byte(`{"question":"What is your name?"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.(string)).To(ContainSubstring("already asked"))
	})

	It("should return error on context timeout", func() {
		store := newTestStore()
		emitter := func(_ context.Context, _ clarify.ClarificationEvent) error { return nil }
		ct := clarify.NewTool(store, emitter)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := ct.Call(ctx, []byte(`{"question":"Timeout question?"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("did not answer"))
	})
})
