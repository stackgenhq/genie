// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hitl_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/hitl/hitlfakes"
	"github.com/stackgenhq/genie/pkg/messenger"
	messengerhitl "github.com/stackgenhq/genie/pkg/messenger/hitl"
	"github.com/stackgenhq/genie/pkg/messenger/messengerfakes"
)

// testOrigin creates a MessageOrigin from the "platform:senderID:channelID" pattern used in tests.
func testOrigin(platform messenger.Platform, senderID, channelID string) messenger.MessageOrigin {
	return messenger.MessageOrigin{
		Platform: platform,
		Sender:   messenger.Sender{ID: senderID},
		Channel:  messenger.Channel{ID: channelID},
	}
}

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

		// FormatApproval should pass through the request unchanged for the fake
		// (text-only adapter behavior). Without this, the counterfeiter stub
		// returns a zero-value SendRequest, wiping the channel and content.
		fakeMessenger.FormatApprovalStub = func(req messenger.SendRequest, _ messenger.ApprovalInfo) messenger.SendRequest {
			return req
		}

		sut = messengerhitl.NewNotifierStore(fakeStore, fakeMessenger)

		req = hitl.CreateRequest{
			ToolName: "write_file",
			Args:     `{"file":"foo.txt"}`,
		}
	})

	Describe("Create", func() {
		var expectedApproval hitl.ApprovalRequest

		BeforeEach(func() {
			expectedApproval = hitl.ApprovalRequest{ID: "app-123", ToolName: "write_file", Args: `{"file":"foo.txt"}`}
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

		Context("when a Slack sender context is present", func() {
			It("sends a plaintext notification (rich formatting is adapter-specific)", func(ctx context.Context) {
				origin := testOrigin(messenger.PlatformSlack, "user1", "C123")
				ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
				resp, err := sut.Create(ctxWithOrigin, req)
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
				senderCtx := origin.String()
				pendingID, found := sut.GetPending(ctx, senderCtx)
				Expect(found).To(BeTrue())
				Expect(pendingID).To(Equal("app-123"))
			})
		})

		Context("when a Teams sender context is present", func() {
			It("sends a plaintext notification (rich formatting is adapter-specific)", func(ctx context.Context) {
				origin := testOrigin(messenger.PlatformTeams, "user2", "T999")
				ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
				resp, err := sut.Create(ctxWithOrigin, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(Equal(expectedApproval))

				// Verify message sent
				Expect(fakeMessenger.SendCallCount()).To(Equal(1))
				_, sendReq := fakeMessenger.SendArgsForCall(0)
				Expect(sendReq.Channel.ID).To(Equal("T999"))
				Expect(sendReq.Content.Text).To(ContainSubstring("write_file"))
				Expect(sendReq.Metadata).To(BeNil())
			})
		})

		Context("when a Google Chat sender context is present", func() {
			It("sends a plaintext notification (rich formatting is adapter-specific)", func(ctx context.Context) {
				origin := testOrigin(messenger.PlatformGoogleChat, "user3", "G456")
				ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
				resp, err := sut.Create(ctxWithOrigin, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(Equal(expectedApproval))

				// Verify message sent
				Expect(fakeMessenger.SendCallCount()).To(Equal(1))
				_, sendReq := fakeMessenger.SendArgsForCall(0)
				Expect(sendReq.Channel.ID).To(Equal("G456"))
				Expect(sendReq.Content.Text).To(ContainSubstring("write_file"))
				Expect(sendReq.Metadata).To(BeNil())
			})
		})

		Context("when a text-only platform sender context is present", func() {
			It("sends a text-only notification with pretty-printed args", func(ctx context.Context) {
				origin := testOrigin(messenger.PlatformDiscord, "user4", "D789")
				ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
				resp, err := sut.Create(ctxWithOrigin, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(Equal(expectedApproval))

				// Verify message sent without rich metadata
				Expect(fakeMessenger.SendCallCount()).To(Equal(1))
				_, sendReq := fakeMessenger.SendArgsForCall(0)
				Expect(sendReq.Channel.ID).To(Equal("D789"))
				Expect(sendReq.Content.Text).To(ContainSubstring("Approval Required"))
				Expect(sendReq.Content.Text).To(ContainSubstring("write_file"))
				// Pretty-printed JSON args should be indented
				Expect(sendReq.Content.Text).To(ContainSubstring("\"file\": \"foo.txt\""))
				// No rich metadata for Discord
				Expect(sendReq.Metadata).To(BeNil())
			})
		})
	})

	Describe("GetPending / RemovePending", func() {
		BeforeEach(func() {
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: "app-456"}, nil)
		})

		It("manages pending state correctly", func(ctx context.Context) {
			origin := testOrigin(messenger.PlatformTeams, "user2", "T999")
			senderCtx := origin.String()

			// Setup: Create an approval
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
			_, err := sut.Create(ctxWithOrigin, hitl.CreateRequest{})
			Expect(err).NotTo(HaveOccurred())

			// Verify retrieval
			val, found := sut.GetPending(ctx, senderCtx)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("app-456"))

			// Verify removal
			sut.RemovePending(ctx, senderCtx)
			_, found = sut.GetPending(ctx, senderCtx)
			Expect(found).To(BeFalse())
		})
	})

	Describe("Resolve", func() {
		It("should delegate to the real store", func(ctx context.Context) {
			fakeStore.ResolveReturns(nil)
			err := sut.Resolve(ctx, hitl.ResolveRequest{ApprovalID: "app-123", Decision: hitl.StatusApproved})
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeStore.ResolveCallCount()).To(Equal(1))
		})
	})

	Describe("WaitForResolution", func() {
		It("should delegate to the real store", func(ctx context.Context) {
			expected := hitl.ApprovalRequest{ID: "app-123", Status: hitl.StatusApproved}
			fakeStore.WaitForResolutionReturns(expected, nil)

			result, err := sut.WaitForResolution(ctx, "app-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(expected))
			Expect(fakeStore.WaitForResolutionCallCount()).To(Equal(1))
		})
	})

	Describe("Close", func() {
		It("should delegate to the real store", func() {
			fakeStore.CloseReturns(nil)
			err := sut.Close()
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeStore.CloseCallCount()).To(Equal(1))
		})
	})

	Describe("IsAllowed", func() {
		It("should delegate to the real store", func() {
			fakeStore.IsAllowedReturns(true)
			result := sut.IsAllowed("bash")
			Expect(result).To(BeTrue())
			Expect(fakeStore.IsAllowedCallCount()).To(Equal(1))
		})

		It("should return false when real store says no", func() {
			fakeStore.IsAllowedReturns(false)
			result := sut.IsAllowed("dangerous_tool")
			Expect(result).To(BeFalse())
		})
	})

	Describe("RecoverPending", func() {
		It("should delegate to the real store", func(ctx context.Context) {
			expected := hitl.RecoverResult{Recovered: 3}
			fakeStore.RecoverPendingReturns(expected, nil)

			result, err := sut.RecoverPending(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(expected))
			Expect(fakeStore.RecoverPendingCallCount()).To(Equal(1))
		})
	})

	Describe("GetPendingByMessageID", func() {
		It("should return empty when no mapping exists", func(ctx context.Context) {
			_, found := sut.GetPendingByMessageID(ctx, "unknown-msg-id")
			Expect(found).To(BeFalse())
		})

		It("should resolve approval ID from message ID after Create with Send", func(ctx context.Context) {
			// Create an approval with messenger that returns a message ID
			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: "app-789"}, nil)
			fakeMessenger.SendReturns(messenger.SendResponse{MessageID: "msg-abc"}, nil)

			origin := testOrigin(messenger.PlatformSlack, "user5", "C456")
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)
			_, err := sut.Create(ctxWithOrigin, req)
			Expect(err).NotTo(HaveOccurred())

			// Now look up approval by message ID
			approvalID, found := sut.GetPendingByMessageID(ctx, "msg-abc")
			Expect(found).To(BeTrue())
			Expect(approvalID).To(Equal("app-789"))
		})
	})

	Describe("RemovePendingByApprovalID", func() {
		It("should remove a specific approval from the queue", func(ctx context.Context) {
			// Create two approvals for the same sender
			origin := testOrigin(messenger.PlatformSlack, "user6", "C789")
			senderCtx := origin.String()
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)

			fakeStore.CreateReturnsOnCall(0, hitl.ApprovalRequest{ID: "app-1"}, nil)
			fakeStore.CreateReturnsOnCall(1, hitl.ApprovalRequest{ID: "app-2"}, nil)

			_, err := sut.Create(ctxWithOrigin, req)
			Expect(err).NotTo(HaveOccurred())
			_, err = sut.Create(ctxWithOrigin, req)
			Expect(err).NotTo(HaveOccurred())

			// Remove the second approval by ID
			sut.RemovePendingByApprovalID(ctx, senderCtx, "app-2")

			// First should still be pending
			pendingID, found := sut.GetPending(ctx, senderCtx)
			Expect(found).To(BeTrue())
			Expect(pendingID).To(Equal("app-1"))
		})

		It("should clean up when queue becomes empty", func(ctx context.Context) {
			origin := testOrigin(messenger.PlatformSlack, "user7", "C999")
			senderCtx := origin.String()
			ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)

			fakeStore.CreateReturns(hitl.ApprovalRequest{ID: "app-only"}, nil)
			_, err := sut.Create(ctxWithOrigin, req)
			Expect(err).NotTo(HaveOccurred())

			sut.RemovePendingByApprovalID(ctx, senderCtx, "app-only")
			_, found := sut.GetPending(ctx, senderCtx)
			Expect(found).To(BeFalse())
		})
	})
})
