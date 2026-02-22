package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/appcd-dev/genie/pkg/clarify"
	"github.com/appcd-dev/genie/pkg/codeowner/codeownerfakes"
	geniedb "github.com/appcd-dev/genie/pkg/db"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/hitl/hitlfakes"
	"github.com/appcd-dev/genie/pkg/messenger"
	messengerhitl "github.com/appcd-dev/genie/pkg/messenger/hitl"
	"github.com/appcd-dev/genie/pkg/messenger/messengerfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Application handleMessengerInput", func() {
	var (
		fakeStore     *hitlfakes.FakeApprovalStore
		fakeMessenger *messengerfakes.FakeMessenger
		fakeCodeOwner *codeownerfakes.FakeCodeOwner
		notifierStore *messengerhitl.NotifierStore
		application   *Application
		ctx           context.Context
		eventChan     chan interface{}
	)

	BeforeEach(func() {
		fakeStore = &hitlfakes.FakeApprovalStore{}
		fakeMessenger = &messengerfakes.FakeMessenger{}
		fakeCodeOwner = &codeownerfakes.FakeCodeOwner{}
		notifierStore = messengerhitl.NewNotifierStore(fakeStore, fakeMessenger)
		ctx = context.Background()
		eventChan = make(chan interface{}, 10)

		// Create a temp DB-backed shortMemory for tests.
		tmpDir := GinkgoT().TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		gormDB, err := geniedb.Open(dbPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(geniedb.AutoMigrate(gormDB)).To(Succeed())
		DeferCleanup(func() {
			_ = geniedb.Close(gormDB)
			_ = os.RemoveAll(tmpDir)
		})

		application = &Application{
			notifierStore: notifierStore,
			msgr:          fakeMessenger,
			codeOwner:     fakeCodeOwner,
			clarifyStore:  clarify.NewStore(gormDB),
			shortMemory:   geniedb.NewShortMemoryStore(gormDB),
			workingDir:    "/tmp/genie-test",
		}
	})

	Context("when a pending approval exists", func() {
		var approvalID string
		var realMsg messenger.IncomingMessage

		BeforeEach(func() {
			approvalID = "app-123"

			realMsg = messenger.IncomingMessage{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}

			// Setup: Create a pending approval
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: approvalID, ToolName: "test_tool"}, nil)

			origin := messenger.MessageOrigin{
				Platform: realMsg.Platform,
				Sender:   realMsg.Sender,
				Channel:  realMsg.Channel,
			}
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
			_, err := notifierStore.Create(ctxWithOrigin, hitl.CreateRequest{ToolName: "test_tool"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("approves when user says 'Yes'", func() {
			realMsg.Content.Text = "Yes"

			application.handleMessengerInput(ctx, realMsg, eventChan)

			// Verify Resolve called
			Expect(fakeStore.ResolveCallCount()).To(Equal(1))
			_, resolveReq := fakeStore.ResolveArgsForCall(0)
			Expect(resolveReq.ApprovalID).To(Equal(approvalID))
			Expect(resolveReq.Decision).To(Equal(hitl.StatusApproved))

			// Verify Confirmation sent (1 for "Approval Required" during Create, 1 for "Approved")
			Expect(fakeMessenger.SendCallCount()).To(Equal(2))
			_, sendReq := fakeMessenger.SendArgsForCall(1)
			Expect(sendReq.Content.Text).To(ContainSubstring("Approved"))

			// Verify it did NOT chat
			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})

		It("rejects when user says 'No'", func() {
			realMsg.Content.Text = "No"

			application.handleMessengerInput(ctx, realMsg, eventChan)

			Expect(fakeStore.ResolveCallCount()).To(Equal(1))
			_, resolveReq := fakeStore.ResolveArgsForCall(0)
			Expect(resolveReq.ApprovalID).To(Equal(approvalID))
			Expect(resolveReq.Decision).To(Equal(hitl.StatusRejected))

			Expect(fakeMessenger.SendCallCount()).To(Equal(2))
			_, sendReq := fakeMessenger.SendArgsForCall(1)
			Expect(sendReq.Content.Text).To(ContainSubstring("Rejected"))

			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})
	})

	Context("when no pending approval exists", func() {
		It("treats 'Yes' as regular chat message", func() {
			msg := messenger.IncomingMessage{
				Content:  messenger.MessageContent{Text: "Yes"},
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			application.handleMessengerInput(ctx, msg, eventChan)

			Eventually(fakeCodeOwner.ChatCallCount).Should(Equal(1))
		})
	})

	Context("when a pending clarification exists", func() {
		var senderCtx string
		var requestID string
		var respCh <-chan clarify.Response
		var realMsg messenger.IncomingMessage

		BeforeEach(func() {
			realMsg = messenger.IncomingMessage{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			senderCtx = realMsg.String()

			// Setup: Create a pending clarification in the store
			var err error
			requestID, respCh, err = application.clarifyStore.Ask(ctx, "What is the target environment?", "", senderCtx)
			Expect(err).NotTo(HaveOccurred())
			_ = application.shortMemory.Set(ctx, "pending_clarification", senderCtx, requestID, 10*time.Minute)
		})

		It("delivers answer and sends confirmation", func() {
			realMsg.Content.Text = "production"

			// Start waiting for the response in a goroutine
			type waitResult struct {
				resp clarify.Response
				err  error
			}
			resultCh := make(chan waitResult, 10)
			waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
			defer waitCancel()
			go func() {
				resp, err := application.clarifyStore.WaitForResponse(waitCtx, requestID, respCh)
				resultCh <- waitResult{resp, err}
			}()

			application.handleMessengerInput(ctx, realMsg, eventChan)

			// Verify answer was delivered
			Eventually(resultCh, 1*time.Second).Should(Receive(WithTransform(
				func(r waitResult) string { return r.resp.Answer },
				Equal("production"),
			)))

			// Verify confirmation reaction sent (👍 on the user's message)
			Expect(fakeMessenger.SendCallCount()).To(Equal(1))
			_, sendReq := fakeMessenger.SendArgsForCall(0)
			Expect(sendReq.Type).To(Equal(messenger.SendTypeReaction))
			Expect(sendReq.Emoji).To(Equal("👍"))

			// Verify the entry was cleaned up
			_, loaded, _ := application.shortMemory.Get(ctx, "pending_clarification", senderCtx)
			Expect(loaded).To(BeFalse())

			// Verify it did NOT reach approval or chat
			Expect(fakeStore.ResolveCallCount()).To(Equal(0))
			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})

		It("takes priority over pending approval", func() {
			// Also set up a pending approval for the same senderCtx
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: "app-456", ToolName: "test_tool"}, nil)
			origin := messenger.MessageOrigin{
				Platform: realMsg.Platform,
				Sender:   realMsg.Sender,
				Channel:  realMsg.Channel,
			}
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
			_, err := notifierStore.Create(ctxWithOrigin, hitl.CreateRequest{ToolName: "test_tool"})
			Expect(err).NotTo(HaveOccurred())

			realMsg.Content.Text = "staging"

			// Start waiting for the response in a goroutine
			type waitResult struct {
				resp clarify.Response
				err  error
			}
			resultCh := make(chan waitResult, 1)
			waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
			defer waitCancel()
			go func() {
				resp, err := application.clarifyStore.WaitForResponse(waitCtx, requestID, respCh)
				resultCh <- waitResult{resp, err}
			}()

			application.handleMessengerInput(ctx, realMsg, eventChan)

			// Should resolve clarification, NOT approval
			Eventually(resultCh, 10*time.Second).Should(Receive(WithTransform(
				func(r waitResult) string { return r.resp.Answer },
				Equal("staging"),
			)))
			Expect(fakeStore.ResolveCallCount()).To(Equal(0))
		})
	})

	Context("when clarification request was already answered", func() {
		It("sends error message for expired request", func() {
			msg := messenger.IncomingMessage{
				Content:  messenger.MessageContent{Text: "my answer"},
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			senderCtx := msg.String()

			// Store a request ID that doesn't exist in the clarify store
			_ = application.shortMemory.Set(ctx, "pending_clarification", senderCtx, "non-existent-id", 10*time.Minute)

			application.handleMessengerInput(ctx, msg, eventChan)

			// Should send error message
			Expect(fakeMessenger.SendCallCount()).To(Equal(1))
			_, sendReq := fakeMessenger.SendArgsForCall(0)
			Expect(sendReq.Content.Text).To(ContainSubstring("Failed to submit answer"))

			// Should NOT reach chat
			Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
		})
	})
})

var _ = Describe("truncateForLog", func() {
	It("should return short strings unchanged", func() {
		Expect(truncateForLog("hello", 10)).To(Equal("hello"))
	})

	It("should return strings at exact max length unchanged", func() {
		Expect(truncateForLog("12345", 5)).To(Equal("12345"))
	})

	It("should truncate long strings with ellipsis", func() {
		Expect(truncateForLog("hello world", 5)).To(Equal("hello..."))
	})

	It("should handle empty string", func() {
		Expect(truncateForLog("", 10)).To(Equal(""))
	})
})
