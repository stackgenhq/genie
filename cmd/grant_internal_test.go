package cmd

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

var _ = Describe("GrantCmd Internal", func() {
	var (
		fakeStore     *hitlfakes.FakeApprovalStore
		fakeMessenger *messengerfakes.FakeMessenger
		fakeCodeOwner *codeownerfakes.FakeCodeOwner
		notifierStore *messengerhitl.NotifierStore
		cmd           *grantCmd
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

		cmd = &grantCmd{
			notifierStore: notifierStore,
			msgr:          fakeMessenger,
			codeOwner:     fakeCodeOwner,
			rootOpts:      &rootCmdOption{workingDir: "/tmp/genie-test"},
		}
	})

	Describe("handleMessengerInput with HITL", func() {
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
					// Content text will be set in It blocks
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

				// Act
				cmd.handleMessengerInput(ctx, realMsg, eventChan)

				// Assert
				// 1. Verify Resolve called
				Expect(fakeStore.ResolveCallCount()).To(Equal(1))
				_, resolveReq := fakeStore.ResolveArgsForCall(0)
				Expect(resolveReq.ApprovalID).To(Equal(approvalID))
				Expect(resolveReq.Decision).To(Equal(hitl.StatusApproved))

				// 2. Verify Confirmation sent
				// We expect 2 calls: 1 for "Approval Required" (during Create in BeforeEach), 1 for "Approved" confirmation
				Expect(fakeMessenger.SendCallCount()).To(Equal(2))
				_, sendReq := fakeMessenger.SendArgsForCall(1)
				Expect(sendReq.Content.Text).To(ContainSubstring("Approved"))

				// Verify it did NOT chat
				Expect(fakeCodeOwner.ChatCallCount()).To(Equal(0))
			})

			It("rejects when user says 'No'", func() {
				realMsg.Content.Text = "No"

				cmd.handleMessengerInput(ctx, realMsg, eventChan)

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
				cmd.handleMessengerInput(ctx, msg, eventChan)

				Eventually(fakeCodeOwner.ChatCallCount).Should(Equal(1))
			})
		})
	})
})
