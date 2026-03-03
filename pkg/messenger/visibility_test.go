package messenger_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("Visibility", func() {
	DescribeTable("DeriveVisibility",
		func(origin messenger.MessageOrigin, expected string) {
			Expect(origin.DeriveVisibility()).To(Equal(expected))
		},
		Entry("zero-value → global",
			messenger.MessageOrigin{}, "global"),
		Entry("WhatsApp DM → private:sender",
			messenger.MessageOrigin{
				Platform: "whatsapp",
				Sender:   messenger.Sender{ID: "5551234567"},
				Channel:  messenger.Channel{ID: "5551234567", Type: messenger.ChannelTypeDM},
			}, "private:5551234567"),
		Entry("WhatsApp group → group:channel",
			messenger.MessageOrigin{
				Platform: "whatsapp",
				Sender:   messenger.Sender{ID: "5551234567"},
				Channel:  messenger.Channel{ID: "120363123456789", Type: messenger.ChannelTypeGroup},
			}, "group:120363123456789"),
		Entry("Slack DM → private:sender",
			messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "D67890", Type: messenger.ChannelTypeDM},
			}, "private:U12345"),
		Entry("Slack channel → group:channel",
			messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "C67890", Type: messenger.ChannelTypeChannel},
			}, "group:C67890"),
		Entry("Slack group DM → group:channel",
			messenger.MessageOrigin{
				Platform: "slack",
				Sender:   messenger.Sender{ID: "U12345"},
				Channel:  messenger.Channel{ID: "G11111", Type: messenger.ChannelTypeGroup},
			}, "group:G11111"),
		Entry("AG-UI (no channel type) → private:sender",
			messenger.MessageOrigin{
				Platform: "agui",
				Sender:   messenger.Sender{ID: "http-user"},
				Channel:  messenger.Channel{ID: "http"},
			}, "private:http-user"),
	)

	Describe("DeriveConversationKey", func() {
		DescribeTable("maps origin to conversation key",
			func(origin messenger.MessageOrigin, expected string) {
				Expect(origin.DeriveConversationKey()).To(Equal(expected))
			},
			Entry("zero-value → global",
				messenger.MessageOrigin{}, "global"),
			Entry("AG-UI with threadId → private:sender:thread",
				messenger.MessageOrigin{
					Platform: "agui",
					Sender:   messenger.Sender{ID: "agui-user"},
					Channel:  messenger.Channel{ID: "thread-abc-123"},
				}, "private:agui-user:thread-abc-123"),
			Entry("no channel ID → private:sender",
				messenger.MessageOrigin{
					Platform: "agui",
					Sender:   messenger.Sender{ID: "agui-user"},
				}, "private:agui-user"),
			Entry("WhatsApp DM → private:sender:channel",
				messenger.MessageOrigin{
					Platform: "whatsapp",
					Sender:   messenger.Sender{ID: "5551234567"},
					Channel:  messenger.Channel{ID: "5551234567", Type: messenger.ChannelTypeDM},
				}, "private:5551234567:5551234567"),
			Entry("WhatsApp group → group:channel",
				messenger.MessageOrigin{
					Platform: "whatsapp",
					Sender:   messenger.Sender{ID: "5551234567"},
					Channel:  messenger.Channel{ID: "120363123456789", Type: messenger.ChannelTypeGroup},
				}, "group:120363123456789"),
			Entry("Slack channel → group:channel",
				messenger.MessageOrigin{
					Platform: "slack",
					Sender:   messenger.Sender{ID: "U12345"},
					Channel:  messenger.Channel{ID: "C67890", Type: messenger.ChannelTypeChannel},
				}, "group:C67890"),
		)

		It("isolates different AG-UI threads from same sender", func() {
			thread1 := messenger.MessageOrigin{
				Platform: "agui",
				Sender:   messenger.Sender{ID: "agui-user"},
				Channel:  messenger.Channel{ID: "thread-1"},
			}
			thread2 := messenger.MessageOrigin{
				Platform: "agui",
				Sender:   messenger.Sender{ID: "agui-user"},
				Channel:  messenger.Channel{ID: "thread-2"},
			}
			Expect(thread1.DeriveConversationKey()).ToNot(Equal(thread2.DeriveConversationKey()))
		})
	})

	DescribeTable("IsPrivateContext",
		func(origin messenger.MessageOrigin, expected bool) {
			Expect(origin.IsPrivateContext()).To(Equal(expected))
		},
		Entry("zero-value (empty channel type) → true",
			messenger.MessageOrigin{}, true),
		Entry("DM channel type → true",
			messenger.MessageOrigin{Channel: messenger.Channel{Type: messenger.ChannelTypeDM}}, true),
		Entry("group channel type → false",
			messenger.MessageOrigin{Channel: messenger.Channel{Type: messenger.ChannelTypeGroup}}, false),
		Entry("channel type → false",
			messenger.MessageOrigin{Channel: messenger.Channel{Type: messenger.ChannelTypeChannel}}, false),
	)

	DescribeTable("IsGroupContext",
		func(origin messenger.MessageOrigin, expected bool) {
			Expect(origin.IsGroupContext()).To(Equal(expected))
		},
		Entry("zero-value → false",
			messenger.MessageOrigin{}, false),
		Entry("group → true",
			messenger.MessageOrigin{Channel: messenger.Channel{Type: messenger.ChannelTypeGroup}}, true),
		Entry("channel → true",
			messenger.MessageOrigin{Channel: messenger.Channel{Type: messenger.ChannelTypeChannel}}, true),
	)
})
