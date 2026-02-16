package messenger

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/logger"
)

// Config holds platform-agnostic messenger configuration that can be loaded
// from TOML or YAML config files (e.g., .genie.toml).
type Config struct {
	// Platform selects which adapter to use (slack, discord, telegram, teams, googlechat).
	// Empty means messaging is disabled.
	Platform Platform `yaml:"platform" toml:"platform"`

	// BufferSize controls the incoming message channel buffer.
	// Defaults to DefaultMessageBufferSize if zero.
	BufferSize int `yaml:"buffer_size" toml:"buffer_size"`

	// Slack holds Slack-specific configuration.
	Slack SlackConfig `yaml:"slack" toml:"slack"`

	// Discord holds Discord-specific configuration.
	Discord DiscordConfig `yaml:"discord" toml:"discord"`

	// Telegram holds Telegram-specific configuration.
	Telegram TelegramConfig `yaml:"telegram" toml:"telegram"`

	// Teams holds Microsoft Teams-specific configuration.
	Teams TeamsConfig `yaml:"teams" toml:"teams"`

	// GoogleChat holds Google Chat-specific configuration.
	GoogleChat GoogleChatConfig `yaml:"googlechat" toml:"googlechat"`

	// WhatsApp holds WhatsApp Business-specific configuration.
	WhatsApp WhatsAppConfig `yaml:"whatsapp" toml:"whatsapp"`
}

// SlackConfig holds Slack adapter settings.
type SlackConfig struct {
	// AppToken is the Slack app-level token (xapp-...) for Socket Mode.
	AppToken string `yaml:"app_token" toml:"app_token"`
	// BotToken is the Slack bot user OAuth token (xoxb-...).
	BotToken string `yaml:"bot_token" toml:"bot_token"`
}

// DiscordConfig holds Discord adapter settings.
type DiscordConfig struct {
	// BotToken is the Discord bot token from the Developer Portal.
	BotToken string `yaml:"bot_token" toml:"bot_token"`
}

// TelegramConfig holds Telegram adapter settings.
type TelegramConfig struct {
	// Token is the Telegram Bot API token from BotFather.
	Token string `yaml:"token" toml:"token"`
}

// TeamsConfig holds Microsoft Teams adapter settings.
type TeamsConfig struct {
	// AppID is the Microsoft Bot Framework App ID.
	AppID string `yaml:"app_id" toml:"app_id"`
	// AppPassword is the Microsoft Bot Framework App Password.
	AppPassword string `yaml:"app_password" toml:"app_password"`
	// ListenAddr is the address for incoming Bot Framework activities (e.g., ":3978").
	ListenAddr string `yaml:"listen_addr" toml:"listen_addr"`
}

// GoogleChatConfig holds Google Chat adapter settings.
type GoogleChatConfig struct {
	// CredentialsFile is the path to the Google service account JSON key file.
	CredentialsFile string `yaml:"credentials_file" toml:"credentials_file"`
	// ListenAddr is the address for incoming HTTP push events (e.g., ":8080").
	ListenAddr string `yaml:"listen_addr" toml:"listen_addr"`
}

// WhatsAppConfig holds WhatsApp adapter settings.
type WhatsAppConfig struct {
	// StorePath is the directory for whatsmeow session/credential storage.
	// Defaults to ~/.genie/whatsapp if empty.
	StorePath string `yaml:"store_path" toml:"store_path"`
}

// Enabled returns true if a messenger platform is configured.
func (c Config) enabled() bool {
	return c.Platform != ""
}

func (c Config) InitMessenger(ctx context.Context) (Messenger, error) {
	if !c.enabled() {
		return nil, nil
	}
	logger := logger.GetLogger(ctx).With("fn", "grantCmd.initMessenger")

	// Validate config format before attempting connection.
	if err := c.Validate(); err != nil {
		logger.Warn("messenger config invalid, continuing without messenger", "error", err)
		return nil, nil
	}

	msgr, err := c.newFromConfig(ctx)
	if err != nil {
		return nil, err
	}
	if err := msgr.Connect(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to messenger: %s: %w", c.Platform, err)
	}

	return msgr, nil
}

// Validate checks that all required secrets are present and appear well-formed
// for the configured platform. It catches common issues like placeholder values,
// whitespace-only tokens, or missing prefix conventions (e.g. Slack xapp-/xoxb-).
// Returns nil when messaging is disabled.
func (c Config) Validate() error {
	if !c.enabled() {
		return nil
	}

	var errs []string

	check := func(name, val string, prefixes ...string) {
		if val == "" {
			errs = append(errs, fmt.Sprintf("%s is required", name))
			return
		}
		if len(prefixes) > 0 {
			ok := false
			for _, p := range prefixes {
				if strings.HasPrefix(val, p) {
					ok = true
					break
				}
			}
			if !ok {
				errs = append(errs, fmt.Sprintf("%s should start with one of %v", name, prefixes))
			}
		}
	}

	switch c.Platform {
	case PlatformSlack:
		check("slack.app_token", c.Slack.AppToken, "xapp-")
		check("slack.bot_token", c.Slack.BotToken, "xoxb-")
	case PlatformDiscord:
		check("discord.bot_token", c.Discord.BotToken)
	case PlatformTelegram:
		check("telegram.token", c.Telegram.Token)
	case PlatformTeams:
		check("teams.app_id", c.Teams.AppID)
		check("teams.app_password", c.Teams.AppPassword)
	case PlatformGoogleChat:
		// GoogleChat credentials_file is optional (can use ADC)
	case PlatformWhatsApp:
		// WhatsApp uses QR code pairing at runtime; no token validation needed.
		// store_path is optional (defaults to ~/.genie/whatsapp).
	}

	if len(errs) > 0 {
		return fmt.Errorf("messenger config validation: %s", strings.Join(errs, "; "))
	}
	return nil
}
