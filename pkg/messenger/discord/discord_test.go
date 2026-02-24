package discord_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/discord"
)

var _ = Describe("Discord Messenger", func() {
	Describe("New", func() {
		It("should create a messenger with a valid-looking token", func() {
			m, err := discord.New(discord.Config{
				BotToken: "MTIzNDU2Nzg5MDEyMzQ1Njc4OQ.XXXXXX.XXXXXXXXXXXXXXXXXXXXXXXX",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
		})

		It("should create a messenger even with an empty token", func() {
			// discordgo.New does not validate the token at creation time.
			m, err := discord.New(discord.Config{BotToken: ""})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
		})
	})

	Describe("Platform", func() {
		It("should return PlatformDiscord", func() {
			m, err := discord.New(discord.Config{
				BotToken: "test-token",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Platform()).To(Equal(messenger.PlatformDiscord))
		})
	})

	Describe("Connection state guards", func() {
		var m *discord.Messenger

		BeforeEach(func() {
			var err error
			m, err = discord.New(discord.Config{
				BotToken: "test-token",
			})
			Expect(err).NotTo(HaveOccurred())
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
			m, err := discord.New(discord.Config{
				BotToken: "test-token",
			})
			Expect(err).NotTo(HaveOccurred())
			var _ messenger.Messenger = m
		})
	})

	Describe("FormatApproval", func() {
		It("should return the request unchanged", func() {
			m, err := discord.New(discord.Config{BotToken: "test-token"})
			Expect(err).NotTo(HaveOccurred())

			req := messenger.SendRequest{
				Channel: messenger.Channel{ID: "C123"},
				Content: messenger.MessageContent{Text: "Approval needed"},
			}
			result := m.FormatApproval(req, messenger.ApprovalInfo{ID: "a1", ToolName: "write_file"})
			Expect(result.Content.Text).To(Equal("Approval needed"))
		})
	})

	Describe("FormatClarification", func() {
		It("should return the request unchanged", func() {
			m, err := discord.New(discord.Config{BotToken: "test-token"})
			Expect(err).NotTo(HaveOccurred())

			req := messenger.SendRequest{
				Channel: messenger.Channel{ID: "C123"},
				Content: messenger.MessageContent{Text: "Question?"},
			}
			result := m.FormatClarification(req, messenger.ClarificationInfo{RequestID: "r1", Question: "what?"})
			Expect(result.Content.Text).To(Equal("Question?"))
		})
	})
})
