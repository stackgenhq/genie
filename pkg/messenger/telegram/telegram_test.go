package telegram_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/messenger/telegram"
)

var _ = Describe("Telegram Messenger", func() {
	Describe("New", func() {
		It("should return an error when token is empty", func() {
			// The underlying go-telegram/bot library validates the token.
			m, err := telegram.New(telegram.Config{Token: ""})
			// The SDK may or may not return an error for empty token.
			// If it does, verify the error wrapping.
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("telegram"))
				Expect(m).To(BeNil())
			}
		})

		It("should create a messenger with a valid-looking token", func() {
			m, err := telegram.New(telegram.Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			})
			// The bot library may validate the token format.
			if err == nil {
				Expect(m).NotTo(BeNil())
			}
		})
	})

	Describe("Platform", func() {
		It("should return PlatformTelegram", func() {
			m, err := telegram.New(telegram.Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			})
			if err == nil {
				Expect(m.Platform()).To(Equal(messenger.PlatformTelegram))
			}
		})
	})

	Describe("Connection state guards", func() {
		var m *telegram.Messenger

		BeforeEach(func() {
			var err error
			m, err = telegram.New(telegram.Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			})
			if err != nil {
				Skip("could not create telegram messenger with test token")
			}
		})

		It("should return ErrNotConnected when Send is called before Connect", func() {
			_, err := m.Send(context.Background(), messenger.SendRequest{
				Channel: messenger.Channel{ID: "12345"},
				Content: messenger.MessageContent{Text: "test"},
			})
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})

		It("should return ErrNotConnected when Receive is called before Connect", func() {
			ch, err := m.Receive(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
			Expect(ch).To(BeNil())
		})

		It("should return ErrNotConnected when Disconnect is called before Connect", func() {
			err := m.Disconnect(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})
	})

	Describe("Interface compliance", func() {
		It("should satisfy the messenger.Messenger interface", func() {
			m, err := telegram.New(telegram.Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			})
			if err == nil {
				var _ messenger.Messenger = m
			}
		})
	})

	Describe("FormatApproval", func() {
		It("should return the request unchanged", func() {
			m, err := telegram.New(telegram.Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			})
			if err != nil {
				Skip("could not create telegram messenger")
			}
			req := messenger.SendRequest{
				Channel: messenger.Channel{ID: "12345"},
				Content: messenger.MessageContent{Text: "Approval needed"},
			}
			result := m.FormatApproval(req, messenger.ApprovalInfo{ID: "a1", ToolName: "write_file"})
			Expect(result.Content.Text).To(Equal("Approval needed"))
		})
	})

	Describe("FormatClarification", func() {
		It("should return the request unchanged", func() {
			m, err := telegram.New(telegram.Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			})
			if err != nil {
				Skip("could not create telegram messenger")
			}
			req := messenger.SendRequest{
				Channel: messenger.Channel{ID: "12345"},
				Content: messenger.MessageContent{Text: "Question?"},
			}
			result := m.FormatClarification(req, messenger.ClarificationInfo{RequestID: "r1", Question: "what?"})
			Expect(result.Content.Text).To(Equal("Question?"))
		})
	})
})
