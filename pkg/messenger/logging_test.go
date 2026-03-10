// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/messengerfakes"
)

var _ = Describe("WithLogging", func() {
	It("should return nil for nil messenger", func() {
		result := messenger.WithLogging(context.Background(), nil)
		Expect(result).To(BeNil())
	})

	It("should wrap a messenger with logging", func() {
		fake := &messengerfakes.FakeMessenger{}
		fake.PlatformReturns(messenger.PlatformSlack)
		wrapped := messenger.WithLogging(context.Background(), fake)
		Expect(wrapped).NotTo(BeNil())
		Expect(wrapped.Platform()).To(Equal(messenger.PlatformSlack))
	})

	Describe("Connect", func() {
		It("should delegate Connect and return no error on success", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.ConnectReturns(nil, nil)
			wrapped := messenger.WithLogging(ctx, fake)

			_, err := wrapped.Connect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(fake.ConnectCallCount()).To(Equal(1))
		})

		It("should log error on Connect failure", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.ConnectReturns(nil, errors.New("connection failed"))
			wrapped := messenger.WithLogging(ctx, fake)

			_, err := wrapped.Connect(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection failed"))
		})
	})

	Describe("Disconnect", func() {
		It("should delegate Disconnect", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.DisconnectReturns(nil)
			wrapped := messenger.WithLogging(ctx, fake)

			err := wrapped.Disconnect(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(fake.DisconnectCallCount()).To(Equal(1))
		})

		It("should log error on Disconnect failure", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.DisconnectReturns(errors.New("disconnect failed"))
			wrapped := messenger.WithLogging(ctx, fake)

			err := wrapped.Disconnect(ctx)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Send", func() {
		It("should delegate Send and return response on success", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.SendReturns(messenger.SendResponse{MessageID: "msg-1"}, nil)
			wrapped := messenger.WithLogging(ctx, fake)

			resp, err := wrapped.Send(ctx, messenger.SendRequest{
				Channel: messenger.Channel{ID: "C123"},
				Content: messenger.MessageContent{Text: "Hello"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.MessageID).To(Equal("msg-1"))
		})

		It("should return error for empty message", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			wrapped := messenger.WithLogging(ctx, fake)

			_, err := wrapped.Send(ctx, messenger.SendRequest{
				Channel: messenger.Channel{ID: "C123"},
				Content: messenger.MessageContent{Text: ""},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty message"))
		})

		It("should log error on Send failure", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.SendReturns(messenger.SendResponse{}, errors.New("send failed"))
			wrapped := messenger.WithLogging(ctx, fake)

			_, err := wrapped.Send(ctx, messenger.SendRequest{
				Channel: messenger.Channel{ID: "C123"},
				Content: messenger.MessageContent{Text: "Hello"},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Receive", func() {
		It("should delegate Receive", func(ctx context.Context) {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			ch := make(chan messenger.IncomingMessage, 1)
			ch <- messenger.IncomingMessage{Content: messenger.MessageContent{Text: "hi"}}
			fake.ReceiveReturns(ch, nil)
			wrapped := messenger.WithLogging(ctx, fake)

			result, err := wrapped.Receive(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			msg := <-result
			Expect(msg.Content.Text).To(Equal("hi"))
		})
	})

	Describe("FormatApproval", func() {
		It("should delegate FormatApproval", func() {
			fake := &messengerfakes.FakeMessenger{}
			fake.PlatformReturns(messenger.PlatformSlack)
			fake.FormatApprovalStub = func(req messenger.SendRequest, info messenger.ApprovalInfo) messenger.SendRequest {
				req.Content.Text = "Approval: " + info.ToolName
				return req
			}
			wrapped := messenger.WithLogging(context.Background(), fake)

			result := wrapped.FormatApproval(
				messenger.SendRequest{},
				messenger.ApprovalInfo{ID: "a1", ToolName: "write_file"},
			)
			Expect(result.Content.Text).To(ContainSubstring("write_file"))
		})
	})
})
