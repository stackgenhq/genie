package messenger_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/messenger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MessageOrigin.String", func() {
	It("should format as platform:senderID:channelID", func() {
		origin := &messenger.MessageOrigin{
			Platform: messenger.PlatformSlack,
			Sender:   messenger.Sender{ID: "user1"},
			Channel:  messenger.Channel{ID: "C123"},
		}
		Expect(origin.String()).To(Equal("slack:user1:C123"))
	})

	It("should return empty string for nil origin", func() {
		var origin *messenger.MessageOrigin
		Expect(origin.String()).To(Equal(""))
	})
})

var _ = Describe("SenderContextFrom", func() {
	It("should return sender context string from context", func() {
		origin := &messenger.MessageOrigin{
			Platform: messenger.PlatformTeams,
			Sender:   messenger.Sender{ID: "user2"},
			Channel:  messenger.Channel{ID: "T999"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), origin)
		Expect(messenger.SenderContextFrom(ctx)).To(Equal("teams:user2:T999"))
	})

	It("should return empty string when no origin in context", func() {
		Expect(messenger.SenderContextFrom(context.Background())).To(Equal(""))
	})
})

var _ = Describe("WithGoal / GoalFromContext", func() {
	It("should store and retrieve goal from context", func() {
		ctx := messenger.WithGoal(context.Background(), "help user with cooking")
		Expect(messenger.GoalFromContext(ctx)).To(Equal("help user with cooking"))
	})

	It("should return empty string when no goal in context", func() {
		Expect(messenger.GoalFromContext(context.Background())).To(Equal(""))
	})
})

var _ = Describe("Config.IsSenderAllowed", func() {
	It("should allow all senders when AllowedSenders is empty", func() {
		cfg := messenger.Config{}
		Expect(cfg.IsSenderAllowed("anyone")).To(BeTrue())
	})

	It("should allow exact match", func() {
		cfg := messenger.Config{AllowedSenders: []string{"user1", "user2"}}
		Expect(cfg.IsSenderAllowed("user1")).To(BeTrue())
		Expect(cfg.IsSenderAllowed("user2")).To(BeTrue())
		Expect(cfg.IsSenderAllowed("user3")).To(BeFalse())
	})

	It("should allow prefix wildcard match", func() {
		cfg := messenger.Config{AllowedSenders: []string{"+1555*"}}
		Expect(cfg.IsSenderAllowed("+15551234567")).To(BeTrue())
		Expect(cfg.IsSenderAllowed("+14161234567")).To(BeFalse())
	})

	It("should reject non-matching senders", func() {
		cfg := messenger.Config{AllowedSenders: []string{"admin"}}
		Expect(cfg.IsSenderAllowed("hacker")).To(BeFalse())
	})
})
