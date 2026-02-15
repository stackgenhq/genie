package messenger_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/messenger"
)

var _ = Describe("Config", func() {
	Describe("enabled (via InitMessenger)", func() {
		It("should return nil for zero-value config", func(ctx context.Context) {
			cfg := messenger.Config{}
			m, err := cfg.InitMessenger(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeNil())
		})

		It("should return nil for empty platform string", func(ctx context.Context) {
			cfg := messenger.Config{Platform: ""}
			m, err := cfg.InitMessenger(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeNil())
		})

		It("should attempt init when platform is set", func(ctx context.Context) {
			// With a platform set but invalid config, InitMessenger will
			// attempt validation and return nil gracefully.
			cfg := messenger.Config{Platform: messenger.PlatformSlack}
			m, err := cfg.InitMessenger(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeNil()) // nil because validation fails (missing tokens)
		})
	})

	Describe("SlackConfig", func() {
		It("should hold app_token and bot_token", func() {
			cfg := messenger.SlackConfig{
				AppToken: "xapp-1-test",
				BotToken: "xoxb-test",
			}
			Expect(cfg.AppToken).To(Equal("xapp-1-test"))
			Expect(cfg.BotToken).To(Equal("xoxb-test"))
		})
	})

	Describe("DiscordConfig", func() {
		It("should hold bot_token", func() {
			cfg := messenger.DiscordConfig{
				BotToken: "MTIzNDU2.XXXXXX.XXXXXXXX",
			}
			Expect(cfg.BotToken).NotTo(BeEmpty())
		})
	})

	Describe("TelegramConfig", func() {
		It("should hold token", func() {
			cfg := messenger.TelegramConfig{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			}
			Expect(cfg.Token).NotTo(BeEmpty())
		})
	})

	Describe("TeamsConfig", func() {
		It("should hold app_id, app_password, and listen_addr", func() {
			cfg := messenger.TeamsConfig{
				AppID:       "app-id-123",
				AppPassword: "secret",
				ListenAddr:  ":3978",
			}
			Expect(cfg.AppID).To(Equal("app-id-123"))
			Expect(cfg.AppPassword).To(Equal("secret"))
			Expect(cfg.ListenAddr).To(Equal(":3978"))
		})
	})

	Describe("GoogleChatConfig", func() {
		It("should hold credentials_file and listen_addr", func() {
			cfg := messenger.GoogleChatConfig{
				CredentialsFile: "/path/to/creds.json",
				ListenAddr:      ":8080",
			}
			Expect(cfg.CredentialsFile).To(Equal("/path/to/creds.json"))
			Expect(cfg.ListenAddr).To(Equal(":8080"))
		})
	})

	Describe("Zero-value defaults", func() {
		It("should have empty/zero defaults for all sub-configs", func() {
			cfg := messenger.Config{}
			Expect(cfg.Platform).To(BeEmpty())
			Expect(cfg.BufferSize).To(Equal(0))
			Expect(cfg.Slack.AppToken).To(BeEmpty())
			Expect(cfg.Slack.BotToken).To(BeEmpty())
			Expect(cfg.Discord.BotToken).To(BeEmpty())
			Expect(cfg.Telegram.Token).To(BeEmpty())
			Expect(cfg.Teams.AppID).To(BeEmpty())
			Expect(cfg.Teams.AppPassword).To(BeEmpty())
			Expect(cfg.Teams.ListenAddr).To(BeEmpty())
			Expect(cfg.GoogleChat.CredentialsFile).To(BeEmpty())
			Expect(cfg.GoogleChat.ListenAddr).To(BeEmpty())
		})
	})

	Describe("Validate", func() {
		It("should return nil when messaging is disabled", func() {
			cfg := messenger.Config{}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should pass for valid Slack config", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformSlack,
				Slack: messenger.SlackConfig{
					AppToken: "xapp-1-test-token",
					BotToken: "xoxb-test-token",
				},
			}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should fail for Slack with missing tokens", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformSlack,
			}
			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("slack.app_token is required"))
			Expect(err.Error()).To(ContainSubstring("slack.bot_token is required"))
		})

		It("should fail for Slack with wrong token prefix", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformSlack,
				Slack: messenger.SlackConfig{
					AppToken: "wrong-prefix",
					BotToken: "also-wrong",
				},
			}
			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should start with"))
		})

		It("should pass for valid Discord config", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformDiscord,
				Discord:  messenger.DiscordConfig{BotToken: "MTIzNDU2.XXXXXX"},
			}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should pass for valid Telegram config", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformTelegram,
				Telegram: messenger.TelegramConfig{Token: "123456:ABC-DEF"},
			}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should pass for valid Teams config", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformTeams,
				Teams: messenger.TeamsConfig{
					AppID:       "app-id-123",
					AppPassword: "secret-pw",
				},
			}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should pass for GoogleChat (optional creds)", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformGoogleChat,
			}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should pass for valid WhatsApp config", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformWhatsApp,
				WhatsApp: messenger.WhatsAppConfig{
					StorePath: "/tmp/whatsapp-test",
				},
			}
			Expect(cfg.Validate()).To(Succeed())
		})

		It("should pass for WhatsApp with empty config (QR pairing at runtime)", func() {
			cfg := messenger.Config{
				Platform: messenger.PlatformWhatsApp,
			}
			Expect(cfg.Validate()).To(Succeed())
		})
	})
})
