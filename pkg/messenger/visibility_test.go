package messenger_test

import (
	"github.com/appcd-dev/genie/pkg/messenger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Visibility", func() {
	Describe("DeriveVisibility", func() {
		It("returns global for nil origin", func() {
			Expect(messenger.DeriveVisibility(nil)).To(Equal("global"))
		})

		It("returns private for WhatsApp DM", func() {
			origin := &messenger.MessageOrigin{
				Platform: "whatsapp",
				Sender:   messenger.Sender{ID: "5551234567"},
				Channel:  messenger.Channel{ID: "5551234567", Type: messenger.ChannelTypeDM},
			}
			Expect(messenger.DeriveVisibility(origin)).To(Equal("private:5551234567"))
		})

		It("returns group for WhatsApp group chat", func() {
			origin := &messenger.MessageOrigin{
				Platform: "whatsapp",
				Sender:   messenger.Sender{ID: "5551234567"},
				Channel:  messenger.Channel{ID: "120363123456789", Type: messenger.ChannelTypeGroup},
			}
			Expect(messenger.DeriveVisibility(origin)).To(Equal("group:120363123456789"))
		})

		It("returns private for Slack DM", func() {
			origin := &messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "D67890", Type: messenger.ChannelTypeDM},
			}
			Expect(messenger.DeriveVisibility(origin)).To(Equal("private:U12345"))
		})

		It("returns group for Slack channel", func() {
			origin := &messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "C67890", Type: messenger.ChannelTypeChannel},
			}
			Expect(messenger.DeriveVisibility(origin)).To(Equal("group:C67890"))
		})

		It("returns group for Slack group DM", func() {
			origin := &messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "G11111", Type: messenger.ChannelTypeGroup},
			}
			Expect(messenger.DeriveVisibility(origin)).To(Equal("group:G11111"))
		})

		It("returns private for AG-UI (no channel type)", func() {
			origin := &messenger.MessageOrigin{
				Platform: "agui",
				Sender:   messenger.Sender{ID: "http-user"},
				Channel:  messenger.Channel{ID: "http"},
			}
			Expect(messenger.DeriveVisibility(origin)).To(Equal("private:http-user"))
		})
	})

	Describe("IsPrivateContext", func() {
		It("returns true for nil origin", func() {
			Expect(messenger.IsPrivateContext(nil)).To(BeTrue())
		})

		It("returns true for DM channel type", func() {
			origin := &messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeDM},
			}
			Expect(messenger.IsPrivateContext(origin)).To(BeTrue())
		})

		It("returns true for empty channel type", func() {
			origin := &messenger.MessageOrigin{}
			Expect(messenger.IsPrivateContext(origin)).To(BeTrue())
		})

		It("returns false for group channel type", func() {
			origin := &messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeGroup},
			}
			Expect(messenger.IsPrivateContext(origin)).To(BeFalse())
		})

		It("returns false for channel type", func() {
			origin := &messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeChannel},
			}
			Expect(messenger.IsPrivateContext(origin)).To(BeFalse())
		})
	})

	Describe("IsGroupContext", func() {
		It("returns false for nil origin", func() {
			Expect(messenger.IsGroupContext(nil)).To(BeFalse())
		})

		It("returns true for group", func() {
			origin := &messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeGroup},
			}
			Expect(messenger.IsGroupContext(origin)).To(BeTrue())
		})

		It("returns true for channel", func() {
			origin := &messenger.MessageOrigin{
				Channel: messenger.Channel{Type: messenger.ChannelTypeChannel},
			}
			Expect(messenger.IsGroupContext(origin)).To(BeTrue())
		})
	})
})
