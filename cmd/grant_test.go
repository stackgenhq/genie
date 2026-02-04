package cmd_test

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/codeowner"
	"github.com/appcd-dev/genie/pkg/codeowner/codeownerfakes"
)

// chatLoopHandler simulates the chat loop logic from grant.go for testing purposes
// This allows us to test the loop logic in isolation
func chatLoopHandler(
	ctx context.Context,
	codeOwner codeowner.CodeOwner,
	userMessages <-chan string,
	eventChan chan<- interface{},
	outputDir string,
) error {
	for {
		select {
		case input, ok := <-userMessages:
			if !ok {
				return nil
			}
			// Process input with ChatExpert
			outputChan := make(chan string)
			go func() {
				codeOwner.Chat(ctx, codeowner.CodeQuestion{
					Question:  input,
					OutputDir: outputDir,
					EventChan: eventChan,
				}, outputChan)
			}()
			for response := range outputChan {
				_ = response // In real code, this emits to eventChan
			}

		case <-ctx.Done():
			return nil
		}
	}
}

var _ = Describe("ChatLoop", func() {
	var (
		mock         *codeownerfakes.FakeCodeOwner
		userMessages chan string
		eventChan    chan interface{}
		ctx          context.Context
		cancel       context.CancelFunc
	)

	BeforeEach(func() {
		mock = &codeownerfakes.FakeCodeOwner{}
		userMessages = make(chan string, 10)
		eventChan = make(chan interface{}, 100)
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	})

	AfterEach(func() {
		cancel()
	})

	Describe("Single Message Handling", func() {
		Context("when chat returns a response", func() {
			BeforeEach(func() {
				mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
					defer close(outputChan)
					outputChan <- "Hello! I see your question: " + req.Question
					return nil
				}
			})

			It("should process the message successfully", func() {
				userMessages <- "what is today's date?"
				close(userMessages)

				err := chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ChatCallCount()).To(Equal(1))
			})
		})

		Context("when chat returns no response", func() {
			BeforeEach(func() {
				mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
					defer close(outputChan)
					// No response sent - channel just closed
					return nil
				}
			})

			It("should complete without error", func() {
				userMessages <- "what is today's date?"
				close(userMessages)

				err := chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ChatCallCount()).To(Equal(1))
			})
		})

		Context("when chat returns an error", func() {
			BeforeEach(func() {
				mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
					defer close(outputChan)
					return errors.New("model error: rate limited")
				}
			})

			It("should complete without error (current behavior - errors are ignored)", func() {
				userMessages <- "what is today's date?"
				close(userMessages)

				// NOTE: The current implementation ignores errors from Chat()!
				// This is a BUG that should be fixed
				err := chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ChatCallCount()).To(Equal(1))
			})
		})
	})

	Describe("Multiple Messages Handling", func() {
		var (
			mu                sync.Mutex
			questionsReceived []string
			responsesCount    int
		)

		BeforeEach(func() {
			questionsReceived = []string{}
			responsesCount = 0

			mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
				defer close(outputChan)
				mu.Lock()
				questionsReceived = append(questionsReceived, req.Question)
				mu.Unlock()
				outputChan <- "Response to: " + req.Question
				mu.Lock()
				responsesCount++
				mu.Unlock()
				return nil
			}
		})

		It("should process all messages sequentially", func() {
			messages := []string{
				"what's today's date?",
				"how many files are there in the directory?",
				"show me the README",
			}

			for _, msg := range messages {
				userMessages <- msg
			}
			close(userMessages)

			err := chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
			Expect(err).NotTo(HaveOccurred())

			mu.Lock()
			defer mu.Unlock()

			Expect(questionsReceived).To(HaveLen(len(messages)))
			Expect(responsesCount).To(Equal(len(messages)))
		})
	})

	Describe("Context Cancellation", func() {
		BeforeEach(func() {
			mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
				defer close(outputChan)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
					outputChan <- "Response"
				}
				return nil
			}
		})

		It("should exit when context is cancelled", func() {
			ctx, cancel := context.WithCancel(context.Background())

			done := make(chan error, 1)
			go func() {
				done <- chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
			}()

			// Cancel context immediately
			cancel()

			Eventually(done, 2*time.Second).Should(Receive(BeNil()))
		})
	})

	Describe("Slow Chat Processing", func() {
		var (
			mu        sync.Mutex
			callOrder []string
		)

		BeforeEach(func() {
			callOrder = []string{}

			mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
				defer close(outputChan)
				mu.Lock()
				callOrder = append(callOrder, "start:"+req.Question)
				mu.Unlock()

				// Simulate processing time
				time.Sleep(50 * time.Millisecond)

				outputChan <- "Done: " + req.Question

				mu.Lock()
				callOrder = append(callOrder, "end:"+req.Question)
				mu.Unlock()
				return nil
			}
		})

		It("should process messages sequentially (not in parallel)", func() {
			userMessages <- "first"
			userMessages <- "second"
			close(userMessages)

			err := chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
			Expect(err).NotTo(HaveOccurred())

			mu.Lock()
			defer mu.Unlock()

			// Chat loop is synchronous, so we expect sequential processing
			expectedOrder := []string{"start:first", "end:first", "start:second", "end:second"}
			Expect(callOrder).To(Equal(expectedOrder))
		})
	})

	Describe("Error Handling (Documents Current Behavior)", func() {
		var (
			chatCalled    bool
			errorReturned bool
		)

		BeforeEach(func() {
			chatCalled = false
			errorReturned = false

			mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
				defer close(outputChan)
				chatCalled = true
				errorReturned = true
				// Return error WITHOUT sending any response
				return errors.New("LLM API error: connection refused")
			}
		})

		It("should NOT surface Chat errors (BUG: errors are silently ignored)", func() {
			userMessages <- "hello"
			close(userMessages)

			// The loop completes without error, even though Chat() returned an error
			err := chatLoopHandler(ctx, mock, userMessages, eventChan, "/tmp/test")

			Expect(chatCalled).To(BeTrue())
			Expect(errorReturned).To(BeTrue())
			Expect(err).NotTo(HaveOccurred()) // BUG: The error is completely ignored!
		})
	})

	Describe("Streaming Responses", func() {
		var (
			mu     sync.Mutex
			chunks []string
		)

		BeforeEach(func() {
			chunks = []string{}

			mock.ChatStub = func(ctx context.Context, req codeowner.CodeQuestion, outputChan chan<- string) error {
				defer close(outputChan)
				// Stream multiple response chunks
				outputChan <- "First chunk"
				outputChan <- "Second chunk"
				outputChan <- "Third chunk"
				return nil
			}
		})

		It("should collect all streaming chunks", func() {
			// Modified handler that captures responses
			captureHandler := func(
				ctx context.Context,
				codeOwner codeowner.CodeOwner,
				userMessages <-chan string,
				eventChan chan<- interface{},
				outputDir string,
			) error {
				for {
					select {
					case input, ok := <-userMessages:
						if !ok {
							return nil
						}
						outputChan := make(chan string)
						go func() {
							codeOwner.Chat(ctx, codeowner.CodeQuestion{
								Question:  input,
								OutputDir: outputDir,
								EventChan: eventChan,
							}, outputChan)
						}()
						for response := range outputChan {
							mu.Lock()
							chunks = append(chunks, response)
							mu.Unlock()
						}
					case <-ctx.Done():
						return nil
					}
				}
			}

			userMessages <- "test"
			close(userMessages)

			err := captureHandler(ctx, mock, userMessages, eventChan, "/tmp/test")
			Expect(err).NotTo(HaveOccurred())

			mu.Lock()
			defer mu.Unlock()

			Expect(chunks).To(HaveLen(3))
			Expect(chunks).To(Equal([]string{"First chunk", "Second chunk", "Third chunk"}))
		})
	})

	Describe("Channel Reuse Bug (Documentation)", func() {
		// This test documents the discovered bug:
		// Reusing the same outputChannel for multiple Chat calls causes a panic
		// because Chat() closes the channel via `defer close(outputChan)`.
		//
		// The grant.go chat loop is CORRECT because it creates a new outputChan
		// for each iteration. But cmd/sabith/main.go had this bug.

		It("documents that channel reuse would cause panic", func() {
			Skip("This test documents a panic scenario - kept for documentation")

			// This would panic with: "send on closed channel" or "close of closed channel"
			// Because Chat() does: defer close(outputChan)
			//
			// WRONG way (causes panic):
			//   outputChannel := make(chan string)
			//   codeOwner.Chat(ctx, q1, outputChannel) // closes outputChannel
			//   codeOwner.Chat(ctx, q2, outputChannel) // PANIC: channel already closed
			//
			// CORRECT way (grant.go does this):
			//   for input := range userMessages {
			//       outputChan := make(chan string) // NEW channel each time
			//       codeOwner.Chat(ctx, q, outputChan)
			//   }
		})
	})
})
