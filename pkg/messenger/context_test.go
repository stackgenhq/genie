package messenger_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("MessageOrigin.String", func() {
	It("should format as platform:senderID:channelID", func() {
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformSlack,
			Sender:   messenger.Sender{ID: "user1"},
			Channel:  messenger.Channel{ID: "C123"},
		}
		Expect(origin.String()).To(Equal("slack:user1:C123"))
	})

	It("should return empty string for zero-value origin", func() {
		origin := messenger.MessageOrigin{}
		Expect(origin.String()).To(Equal(""))
	})
})

var _ = Describe("MessageOriginFrom", func() {
	It("should return origin struct from context", func() {
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformTeams,
			Sender:   messenger.Sender{ID: "user2"},
			Channel:  messenger.Channel{ID: "T999"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), origin)
		got := messenger.MessageOriginFrom(ctx)
		Expect(got).NotTo(BeZero())
		Expect(got.Platform).To(Equal(messenger.PlatformTeams))
		Expect(got.Sender.ID).To(Equal("user2"))
		Expect(got.Channel.ID).To(Equal("T999"))
		Expect(got.String()).To(Equal("teams:user2:T999"))
	})

	It("should return zero-value when no origin in context", func() {
		got := messenger.MessageOriginFrom(context.Background())
		Expect(got.IsZero()).To(BeTrue())
	})
})

var _ = Describe("WithMessageOrigin overwrite semantics", func() {
	It("should set origin on a fresh context", func() {
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformSlack,
			Sender:   messenger.Sender{ID: "user1"},
			Channel:  messenger.Channel{ID: "C123"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), origin)
		got := messenger.MessageOriginFrom(ctx)
		Expect(got.Platform).To(Equal(messenger.PlatformSlack))
		Expect(got.Sender.ID).To(Equal("user1"))
	})

	It("should NOT overwrite a non-system origin", func() {
		first := messenger.MessageOrigin{
			Platform: messenger.PlatformSlack,
			Sender:   messenger.Sender{ID: "real-user"},
			Channel:  messenger.Channel{ID: "C123"},
		}
		second := messenger.MessageOrigin{
			Platform: messenger.PlatformAGUI,
			Sender:   messenger.Sender{ID: "agui-user"},
			Channel:  messenger.Channel{ID: "thread-1"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), first)
		ctx = messenger.WithMessageOrigin(ctx, second) // should be a no-op
		got := messenger.MessageOriginFrom(ctx)
		Expect(got.Platform).To(Equal(messenger.PlatformSlack))
		Expect(got.Sender.ID).To(Equal("real-user"))
		Expect(got.Channel.ID).To(Equal("C123"))
	})

	It("should overwrite a system origin", func() {
		systemOrigin := messenger.SystemMessageOrigin()
		realOrigin := messenger.MessageOrigin{
			Platform: messenger.PlatformSlack,
			Sender:   messenger.Sender{ID: "real-user"},
			Channel:  messenger.Channel{ID: "C456"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), systemOrigin)
		ctx = messenger.WithMessageOrigin(ctx, realOrigin) // should overwrite
		got := messenger.MessageOriginFrom(ctx)
		Expect(got.Platform).To(Equal(messenger.PlatformSlack))
		Expect(got.Sender.ID).To(Equal("real-user"))
		Expect(got.Channel.ID).To(Equal("C456"))
	})

	It("should set origin when context has zero-value origin", func() {
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformTeams,
			Sender:   messenger.Sender{ID: "user2"},
			Channel:  messenger.Channel{ID: "T999"},
		}
		ctx := messenger.WithMessageOrigin(context.Background(), origin)
		got := messenger.MessageOriginFrom(ctx)
		Expect(got.Platform).To(Equal(messenger.PlatformTeams))
	})
})

var _ = Describe("SystemMessageOrigin", func() {
	It("should return an origin with all fields set to system", func() {
		origin := messenger.SystemMessageOrigin()
		Expect(origin.Platform).To(Equal(messenger.Platform("system")))
		Expect(origin.Sender.ID).To(Equal("system"))
		Expect(origin.Channel.ID).To(Equal("system"))
	})

	It("should be recognized by IsSystem", func() {
		Expect(messenger.SystemMessageOrigin().IsSystem()).To(BeTrue())
	})

	It("should not be zero", func() {
		Expect(messenger.SystemMessageOrigin().IsZero()).To(BeFalse())
	})
})

var _ = Describe("MessageOrigin.IsSystem", func() {
	It("returns false for zero-value", func() {
		Expect(messenger.MessageOrigin{}.IsSystem()).To(BeFalse())
	})

	It("returns false for a real user origin", func() {
		origin := messenger.MessageOrigin{
			Platform: messenger.PlatformSlack,
			Sender:   messenger.Sender{ID: "user1"},
			Channel:  messenger.Channel{ID: "C123"},
		}
		Expect(origin.IsSystem()).To(BeFalse())
	})

	It("returns false when only some fields are system", func() {
		partial := messenger.MessageOrigin{
			Platform: "system",
			Sender:   messenger.Sender{ID: "not-system"},
			Channel:  messenger.Channel{ID: "system"},
		}
		Expect(partial.IsSystem()).To(BeFalse())
	})

	It("returns true for SystemMessageOrigin", func() {
		Expect(messenger.SystemMessageOrigin().IsSystem()).To(BeTrue())
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
