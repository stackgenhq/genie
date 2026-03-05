package discord

import (
	"context"

	"github.com/bwmarrin/discordgo"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("Discord Internal", func() {
	Describe("messageCreate", func() {
		It("ignores self messages", func() {
			m, err := New(Config{BotToken: "test"})
			Expect(err).To(Not(HaveOccurred()))

			m.session.State.User = &discordgo.User{ID: "bot1"}
			m.incoming = make(chan messenger.IncomingMessage, 10)

			event := &discordgo.MessageCreate{
				Message: &discordgo.Message{
					Author: &discordgo.User{ID: "bot1"},
				},
			}
			m.messageCreate(m.session, event)

			Consistently(m.incoming).ShouldNot(Receive())
		})

		It("emits message events for user messages", func() {
			m, err := New(Config{BotToken: "test"})
			Expect(err).To(Not(HaveOccurred()))

			m.session.State.User = &discordgo.User{ID: "bot1"}
			m.incoming = make(chan messenger.IncomingMessage, 10)
			m.connCtx = context.Background()

			event := &discordgo.MessageCreate{
				Message: &discordgo.Message{
					Author:  &discordgo.User{ID: "user1", Username: "user1"},
					Content: "hello",
				},
			}
			m.messageCreate(m.session, event)

			var inc messenger.IncomingMessage
			Eventually(m.incoming).Should(Receive(&inc))
			Expect(inc.Content.Text).To(Equal("hello"))
			Expect(inc.Sender.ID).To(Equal("user1"))
		})
	})

	Describe("UpdateMessage", func() {
		It("returns nil", func() {
			m := &Messenger{}
			err := m.UpdateMessage(context.Background(), messenger.UpdateRequest{})
			Expect(err).To(BeNil())
		})
	})
})
