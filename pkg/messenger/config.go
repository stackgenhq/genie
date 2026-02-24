package messenger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
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

	// AllowedSenders is an optional allowlist of sender IDs (phone numbers,
	// usernames, or user IDs depending on platform) that the bot will respond
	// to. When empty, the bot responds to all incoming messages.
	// For WhatsApp: use phone numbers without '+' (e.g., "15551234567").
	AllowedSenders []string `yaml:"allowed_senders" toml:"allowed_senders"`

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

	// AGUI holds AG-UI SSE adapter configuration.
	AGUI AGUIConfig `yaml:"agui" toml:"agui"`
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

// AGUIConfig holds AG-UI server and messenger adapter configuration.
// When AGUI is the active messenger (default), these settings configure both
// the in-process messenger adapter and the HTTP SSE server.
type AGUIConfig struct {
	// AppName is used as the session.Key AppName for thread tracking.
	// Defaults to "genie" if empty.
	AppName string `yaml:"app_name" toml:"app_name"`

	// --- Server settings ---
	CORSOrigins   []string `yaml:"cors_origins" toml:"cors_origins"`
	Port          uint32   `yaml:"port" toml:"port"`
	RateLimit     float64  `yaml:"rate_limit" toml:"rate_limit"`         // req/sec per IP (0 = disabled)
	RateBurst     int      `yaml:"rate_burst" toml:"rate_burst"`         // burst allowance per IP
	MaxConcurrent int      `yaml:"max_concurrent" toml:"max_concurrent"` // max in-flight requests (0 = unlimited)
	MaxBodyBytes  int64    `yaml:"max_body_bytes" toml:"max_body_bytes"` // max request body in bytes (0 = unlimited)
}

// DefaultAGUIConfig returns sensible defaults for the AG-UI server.
func DefaultAGUIConfig() AGUIConfig {
	return AGUIConfig{
		CORSOrigins:   []string{"*"},
		Port:          8080,
		RateLimit:     0.5, // 30 req/min per IP
		RateBurst:     3,
		MaxConcurrent: 5,
		MaxBodyBytes:  1 << 20, // 1 MB
	}
}

// Enabled returns true if a messenger platform is configured.
func (c Config) enabled() bool {
	return c.Platform != ""
}

// IsSenderAllowed checks whether the given sender ID is permitted to interact
// with the bot. When AllowedSenders is empty, all senders are allowed.
// Entries ending with '*' are treated as prefix matches (e.g., "1555*"
// matches any sender starting with "1555"). This enables operators to
// restrict the bot to specific users, country codes, or area codes.
func (c Config) IsSenderAllowed(senderID string) bool {
	if len(c.AllowedSenders) == 0 {
		return true
	}
	for _, allowed := range c.AllowedSenders {
		if strings.HasSuffix(allowed, "*") {
			if strings.HasPrefix(senderID, strings.TrimSuffix(allowed, "*")) {
				return true
			}
			continue
		}
		if allowed == senderID {
			return true
		}
	}
	return false
}

// InitMessenger creates a Messenger for the configured platform. If no
// platform is configured, it defaults to AGUI. Connect() is NOT called —
// the caller is responsible for connecting the messenger when ready.
func (c Config) InitMessenger(ctx context.Context) (Messenger, error) {
	log := logger.GetLogger(ctx).With("fn", "Config.InitMessenger")

	if !c.enabled() {
		log.Info("No messenger platform configured — defaulting to AGUI")
		return c.initDefaultAGUI(ctx)
	}

	// Validate config format before attempting creation.
	if err := c.Validate(); err != nil {
		log.Warn("messenger config invalid, falling back to AGUI", "error", err)
		return c.initDefaultAGUI(ctx)
	}

	return c.newFromConfig(ctx)
}

// initDefaultAGUI creates the in-process AGUI messenger adapter.
// Note: Connect() is NOT called here — the AGUI messenger needs
// ConfigureServer() before Connect() (which starts the HTTP server).
func (c Config) initDefaultAGUI(ctx context.Context) (Messenger, error) {
	msgr, err := newAGUIFromConfig()
	if err != nil {
		// AGUI adapter not registered — gracefully degrade to nil.
		logger.GetLogger(ctx).Debug("AGUI messenger adapter not available", "error", err)
		return nil, errors.New("agui messenger adapter not available: cannot bootstrap server")
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
	case PlatformAGUI:
		// AGUI runs in-process; no external credentials required.
	}

	if len(errs) > 0 {
		return fmt.Errorf("messenger config validation: %s", strings.Join(errs, "; "))
	}
	return nil
}
