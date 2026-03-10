// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("Messenger Types", func() {
	Describe("Platform constants", func() {
		It("should have the expected platform identifiers", func() {
			Expect(string(messenger.PlatformSlack)).To(Equal("slack"))
			Expect(string(messenger.PlatformDiscord)).To(Equal("discord"))
			Expect(string(messenger.PlatformTelegram)).To(Equal("telegram"))
		})
	})

	Describe("ChannelType constants", func() {
		It("should have the expected channel type identifiers", func() {
			Expect(string(messenger.ChannelTypeDM)).To(Equal("dm"))
			Expect(string(messenger.ChannelTypeGroup)).To(Equal("group"))
			Expect(string(messenger.ChannelTypeChannel)).To(Equal("channel"))
		})
	})

	Describe("SendRequest", func() {
		It("should hold all required fields", func() {
			req := messenger.SendRequest{
				Channel: messenger.Channel{
					ID:   "C12345",
					Name: "general",
					Type: messenger.ChannelTypeChannel,
				},
				Content: messenger.MessageContent{
					Text: "Hello, world!",
					Attachments: []messenger.Attachment{
						{
							Name:        "report.pdf",
							URL:         "https://example.com/report.pdf",
							ContentType: "application/pdf",
							Size:        1024,
						},
					},
				},
				ThreadID: "1234567890.123456",
				Metadata: map[string]any{"priority": "high"},
			}

			Expect(req.Channel.ID).To(Equal("C12345"))
			Expect(req.Channel.Name).To(Equal("general"))
			Expect(req.Channel.Type).To(Equal(messenger.ChannelTypeChannel))
			Expect(req.Content.Text).To(Equal("Hello, world!"))
			Expect(req.Content.Attachments).To(HaveLen(1))
			Expect(req.Content.Attachments[0].Name).To(Equal("report.pdf"))
			Expect(req.Content.Attachments[0].Size).To(Equal(int64(1024)))
			Expect(req.ThreadID).To(Equal("1234567890.123456"))
			Expect(req.Metadata).To(HaveKeyWithValue("priority", "high"))
		})
	})

	Describe("IncomingMessage", func() {
		It("should represent a fully populated incoming message", func() {
			now := time.Now()
			msg := messenger.IncomingMessage{
				ID:       "msg-001",
				Platform: messenger.PlatformSlack,
				Channel: messenger.Channel{
					ID:   "C12345",
					Name: "general",
					Type: messenger.ChannelTypeChannel,
				},
				Sender: messenger.Sender{
					ID:          "U12345",
					Username:    "jdoe",
					DisplayName: "Jane Doe",
				},
				Content: messenger.MessageContent{
					Text: "Deploy to staging please",
				},
				ThreadID:  "1234567890.123456",
				Timestamp: now,
				Metadata:  map[string]any{"source": "bot-mention"},
			}

			Expect(msg.ID).To(Equal("msg-001"))
			Expect(msg.Platform).To(Equal(messenger.PlatformSlack))
			Expect(msg.Sender.DisplayName).To(Equal("Jane Doe"))
			Expect(msg.Content.Text).To(Equal("Deploy to staging please"))
			Expect(msg.ThreadID).NotTo(BeEmpty())
			Expect(msg.Timestamp).To(BeTemporally("~", now, time.Second))
		})
	})
})

var _ = Describe("Options", func() {
	Describe("DefaultAdapterConfig", func() {
		It("should return sensible defaults", func() {
			cfg := messenger.DefaultAdapterConfig()
			Expect(cfg.MessageBufferSize).To(Equal(100))
		})
	})

	Describe("ApplyOptions", func() {
		It("should apply WithMessageBuffer", func() {
			cfg := messenger.ApplyOptions(messenger.WithMessageBuffer(500))
			Expect(cfg.MessageBufferSize).To(Equal(500))
		})

		It("should ignore non-positive buffer size", func() {
			cfg := messenger.ApplyOptions(messenger.WithMessageBuffer(0))
			Expect(cfg.MessageBufferSize).To(Equal(100))

			cfg = messenger.ApplyOptions(messenger.WithMessageBuffer(-1))
			Expect(cfg.MessageBufferSize).To(Equal(100))
		})

		It("should apply multiple options", func() {
			cfg := messenger.ApplyOptions(
				messenger.WithMessageBuffer(250),
			)
			Expect(cfg.MessageBufferSize).To(Equal(250))
		})
	})
})

var _ = Describe("Errors", func() {
	It("should have distinct error messages", func() {
		Expect(messenger.ErrNotConnected.Error()).To(ContainSubstring("not connected"))
		Expect(messenger.ErrAlreadyConnected.Error()).To(ContainSubstring("already connected"))
		Expect(messenger.ErrChannelNotFound.Error()).To(ContainSubstring("channel not found"))
		Expect(messenger.ErrSendFailed.Error()).To(ContainSubstring("send failed"))
		Expect(messenger.ErrRateLimited.Error()).To(ContainSubstring("rate limited"))
	})
})
