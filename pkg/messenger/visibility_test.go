package messenger_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("Visibility", func() {
	Describe("DeriveVisibility", func() {
		It("returns global for zero-value origin", func() {
			origin := messenger.MessageOrigin{}
			Expect(origin.DeriveVisibility()).To(Equal("global"))
		})

		It("returns private for WhatsApp DM", func() {
			origin := messenger.MessageOrigin{
				Platform: "whatsapp",
				Sender:   messenger.Sender{ID: "5551234567"},
				Channel:  messenger.Channel{ID: "5551234567", Type: messenger.ChannelTypeDM},
			}
			Expect(origin.DeriveVisibility()).To(Equal("private:5551234567"))
		})

		It("returns group for WhatsApp group chat", func() {
			origin := messenger.MessageOrigin{
				Platform: "whatsapp",
				Sender:   messenger.Sender{ID: "5551234567"},
				Channel:  messenger.Channel{ID: "120363123456789", Type: messenger.ChannelTypeGroup},
			}
			Expect(origin.DeriveVisibility()).To(Equal("group:120363123456789"))
		})

		It("returns private for Slack DM", func() {
			origin := messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "D67890", Type: messenger.ChannelTypeDM},
			}
			Expect(origin.DeriveVisibility()).To(Equal("private:U12345"))
		})

		It("returns group for Slack channel", func() {
			origin := messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "C67890", Type: messenger.ChannelTypeChannel},
			}
			Expect(origin.DeriveVisibility()).To(Equal("group:C67890"))
		})

		It("returns group for Slack group DM", func() {
			origin := messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "G11111", Type: messenger.ChannelTypeGroup},
			}
			Expect(origin.DeriveVisibility()).To(Equal("group:G11111"))
		})

		It("returns private for AG-UI (no channel type)", func() {
			origin := messenger.MessageOrigin{
				Platform: "agui",
				Sender:   messenger.Sender{ID: "http-user"},
				Channel:  messenger.Channel{ID: "http"},
			}
			Expect(origin.DeriveVisibility()).To(Equal("private:http-user"))
		})
	})

	Describe("IsPrivateContext", func() {
		It("returns true for zero-value origin", func() {
			origin := messenger.MessageOrigin{}
			Expect(origin.IsPrivateContext()).To(BeTrue())
		})

		It("returns true for DM channel type", func() {
			origin := messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeDM},
			}
			Expect(origin.IsPrivateContext()).To(BeTrue())
		})

		It("returns true for empty channel type", func() {
			origin := messenger.MessageOrigin{}
			Expect(origin.IsPrivateContext()).To(BeTrue())
		})

		It("returns false for group channel type", func() {
			origin := messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeGroup},
			}
			Expect(origin.IsPrivateContext()).To(BeFalse())
		})

		It("returns false for channel type", func() {
			origin := messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeChannel},
			}
			Expect(origin.IsPrivateContext()).To(BeFalse())
		})
	})

	Describe("IsGroupContext", func() {
		It("returns false for zero-value origin", func() {
			origin := messenger.MessageOrigin{}
			Expect(origin.IsGroupContext()).To(BeFalse())
		})

		It("returns true for group", func() {
			origin := messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeGroup},
			}
			Expect(origin.IsGroupContext()).To(BeTrue())
		})

		It("returns true for channel", func() {
			origin := messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeChannel},
			}
			Expect(origin.IsGroupContext()).To(BeTrue())
		})
	})
})
