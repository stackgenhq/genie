package app

import (
	"context"

	"github.com/appcd-dev/genie/pkg/codeowner/codeownerfakes"
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

		application = &Application{
			notifierStore: notifierStore,
			msgr:          fakeMessenger,
			codeOwner:     fakeCodeOwner,
			workingDir:    "/tmp/genie-test",
		}
	})

	Context("when a pending approval exists", func() {
		var senderCtx string
		var approvalID string
		var realMsg messenger.IncomingMessage

		BeforeEach(func() {
			approvalID = "app-123"

			realMsg = messenger.IncomingMessage{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "user1"},
				Channel:  messenger.Channel{ID: "C123"},
			}
			senderCtx = realMsg.String()

			// Setup: Create a pending approval
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: approvalID, ToolName: "test_tool"}, nil)

			ctxWithSender := messenger.WithSenderContext(ctx, senderCtx)
			_, err := notifierStore.Create(ctxWithSender, hitl.CreateRequest{ToolName: "test_tool"})
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
})
