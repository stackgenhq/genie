package telegram

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"

	tgmodels "github.com/go-telegram/bot/models"
)

var _ = Describe("Telegram Internal", func() {
	Describe("reactionEmojiFromTypes", func() {
		It("returns correctly", func() {
			val := reactionEmojiFromTypes([]tgmodels.ReactionType{
				{
					Type:              tgmodels.ReactionTypeTypeEmoji,
					ReactionTypeEmoji: &tgmodels.ReactionTypeEmoji{Emoji: "👍"},
				},
			})
			Expect(val).To(Equal("👍"))
		})
		It("returns empty string if none", func() {
			val := reactionEmojiFromTypes([]tgmodels.ReactionType{})
			Expect(val).To(Equal(""))
		})
	})
	Describe("senderFromReactionActor", func() {
		It("returns sender from user", func() {
			m := &Messenger{}
			r := &tgmodels.MessageReactionUpdated{
				User: &tgmodels.User{
					ID:        123,
					Username:  "testuser",
					FirstName: "Test",
					LastName:  "User",
				},
			}
			sender := m.senderFromReactionActor(r)
			Expect(sender.ID).To(Equal("123"))
			Expect(sender.Username).To(Equal("testuser"))
			Expect(sender.DisplayName).To(Equal("Test User"))
		})
		It("returns sender from actor chat", func() {
			m := &Messenger{}
			r := &tgmodels.MessageReactionUpdated{
				ActorChat: &tgmodels.Chat{
					ID:    456,
					Title: "Test Chat",
				},
			}
			sender := m.senderFromReactionActor(r)
			Expect(sender.ID).To(Equal("456"))
			Expect(sender.DisplayName).To(Equal("Test Chat"))
		})
		It("returns unknown if none", func() {
			m := &Messenger{}
			r := &tgmodels.MessageReactionUpdated{}
			sender := m.senderFromReactionActor(r)
			Expect(sender.ID).To(Equal("unknown"))
		})
	})

	Describe("No-Op and formatting methods", func() {
		It("returns correct Platform", func() {
			m := &Messenger{}
			Expect(m.Platform()).To(Equal(messenger.PlatformTelegram))
		})

		It("returns correct ConnectionInfo", func() {
			m := &Messenger{}
			Expect(m.ConnectionInfo()).To(ContainSubstring("Telegram"))
		})

		It("returns UpdateMessage nil", func() {
			m := &Messenger{}
			err := m.UpdateMessage(context.Background(), messenger.UpdateRequest{})
			Expect(err).To(BeNil())
		})

		It("formats approval with no change", func() {
			m := &Messenger{}
			req := messenger.SendRequest{}
			req.Content.Text = "foo"
			formatted := m.FormatApproval(req, messenger.ApprovalInfo{})
			Expect(formatted.Content.Text).To(Equal("foo"))
		})

		It("formats clarification with no change", func() {
			m := &Messenger{}
			req := messenger.SendRequest{}
			req.Content.Text = "bar"
			formatted := m.FormatClarification(req, messenger.ClarificationInfo{})
			Expect(formatted.Content.Text).To(Equal("bar"))
		})
	})
})
