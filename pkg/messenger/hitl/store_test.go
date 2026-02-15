package hitl_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/hitl/hitlfakes"
	"github.com/appcd-dev/genie/pkg/messenger"
	messengerhitl "github.com/appcd-dev/genie/pkg/messenger/hitl"
	"github.com/appcd-dev/genie/pkg/messenger/messengerfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NotifierStore", func() {
	var (
		fakeStore     *hitlfakes.FakeApprovalStore
		fakeMessenger *messengerfakes.FakeMessenger
		sut           *messengerhitl.NotifierStore
		req           hitl.CreateRequest
	)

	BeforeEach(func() {
		fakeStore = &hitlfakes.FakeApprovalStore{}
		fakeMessenger = &messengerfakes.FakeMessenger{}
		sut = messengerhitl.NewNotifierStore(fakeStore, fakeMessenger)

		req = hitl.CreateRequest{
			ToolName: "write_file",
			Args:     `{"file":"foo.txt"}`,
		}
	})

	Describe("Create", func() {
		var expectedApproval hitl.ApprovalRequest

		BeforeEach(func() {
			expectedApproval = hitl.ApprovalRequest{ID: "app-123", ToolName: "write_file"}
			fakeStore.CreateReturns(expectedApproval, nil)
		})

		Context("when no sender context is present", func() {
			It("calls the underlying store but does not send a message", func(ctx context.Context) {
				resp, err := sut.Create(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(Equal(expectedApproval))
				Expect(fakeMessenger.SendCallCount()).To(Equal(0))
			})
		})

		Context("when a valid sender context is present", func() {
			var senderCtx string

			BeforeEach(func() {
				senderCtx = "slack:user1:C123"
			})

			It("calls the store, sends a notification, and stores pending approval", func(ctx context.Context) {
				ctxWithSender := messenger.WithSenderContext(ctx, senderCtx)
				resp, err := sut.Create(ctxWithSender, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(Equal(expectedApproval))

				// Verify store call
				Expect(fakeStore.CreateCallCount()).To(Equal(1))

				// Verify message sent
				Expect(fakeMessenger.SendCallCount()).To(Equal(1))
				_, sendReq := fakeMessenger.SendArgsForCall(0)
				Expect(sendReq.Channel.ID).To(Equal("C123"))
				Expect(sendReq.Content.Text).To(ContainSubstring("Approval Required"))
				Expect(sendReq.Content.Text).To(ContainSubstring("write_file"))

				// Verify pending mapping
				pendingID, found := sut.GetPending(senderCtx)
				Expect(found).To(BeTrue())
				Expect(pendingID).To(Equal("app-123"))
			})
		})
	})

	Describe("GetPending / RemovePending", func() {
		var senderCtx string

		BeforeEach(func() {
			senderCtx = "teams:user2:T999"
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: "app-456"}, nil)
		})

		It("manages pending state correctly", func(ctx context.Context) {
			// Setup: Create an approval
			ctxWithSender := messenger.WithSenderContext(ctx, senderCtx)
			_, err := sut.Create(ctxWithSender, hitl.CreateRequest{})
			Expect(err).NotTo(HaveOccurred())

			// Verify retrieval
			val, found := sut.GetPending(senderCtx)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("app-456"))

			// Verify removal
			sut.RemovePending(senderCtx)
			_, found = sut.GetPending(senderCtx)
			Expect(found).To(BeFalse())
		})
	})
})
