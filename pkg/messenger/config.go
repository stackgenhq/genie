// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security/auth"
)

const DefaultAGUIPort uint32 = 9876

// Config holds platform-agnostic messenger configuration that can be loaded
// from TOML or YAML config files (e.g., .genie.toml).
type Config struct {
	// Platform selects which adapter to use (slack, discord, telegram, teams, googlechat).
	// Empty means messaging is disabled.
	Platform Platform `yaml:"platform,omitempty" toml:"platform,omitempty"`

	// BufferSize controls the incoming message channel buffer.
	// Defaults to DefaultMessageBufferSize if zero.
	BufferSize int `yaml:"buffer_size,omitempty" toml:"buffer_size,omitempty"`

	// AllowedSenders is an optional allowlist of sender IDs (phone numbers,
	// usernames, or user IDs depending on platform) that the bot will respond
	// to. When empty, the bot responds to all incoming messages.
	// For WhatsApp: use phone numbers without '+' (e.g., "15551234567").
	AllowedSenders []string `yaml:"allowed_senders,omitempty" toml:"allowed_senders,omitempty"`

	// Slack holds Slack-specific configuration.
	Slack SlackConfig `yaml:"slack,omitempty" toml:"slack,omitempty"`

	// Discord holds Discord-specific configuration.
	Discord DiscordConfig `yaml:"discord,omitempty" toml:"discord,omitempty"`

	// Telegram holds Telegram-specific configuration.
	Telegram TelegramConfig `yaml:"telegram,omitempty" toml:"telegram,omitempty"`

	// Teams holds Microsoft Teams-specific configuration.
	Teams TeamsConfig `yaml:"teams,omitempty" toml:"teams,omitempty"`

	// GoogleChat holds Google Chat-specific configuration.
	GoogleChat GoogleChatConfig `yaml:"googlechat,omitempty" toml:"googlechat,omitempty"`

	// WhatsApp holds WhatsApp Business-specific configuration.
	WhatsApp WhatsAppConfig `yaml:"whatsapp,omitempty" toml:"whatsapp,omitempty"`

	// AGUI holds AG-UI SSE adapter configuration.
	AGUI AGUIConfig `yaml:"agui,omitempty" toml:"agui,omitempty"`
}

// SlackConfig holds Slack adapter settings.
type SlackConfig struct {
	// AppToken is the Slack app-level token (xapp-...) for Socket Mode.
	AppToken string `yaml:"app_token,omitempty" toml:"app_token,omitempty"`
	// BotToken is the Slack bot user OAuth token (xoxb-...).
	BotToken string `yaml:"bot_token,omitempty" toml:"bot_token,omitempty"`
	// RespondTo controls when the bot processes messages in channels:
	//   - "mentions" or "" (default): only respond when @mentioned or in threads where previously mentioned
	//   - "all": respond to every message
	RespondTo string `yaml:"respond_to,omitempty" toml:"respond_to,omitempty"`
}

// DiscordConfig holds Discord adapter settings.
type DiscordConfig struct {
	// BotToken is the Discord bot token from the Developer Portal.
	BotToken string `yaml:"bot_token,omitempty" toml:"bot_token,omitempty"`
}

// TelegramConfig holds Telegram adapter settings.
type TelegramConfig struct {
	// Token is the Telegram Bot API token from BotFather.
	Token string `yaml:"token,omitempty" toml:"token,omitempty"`
}

// TeamsConfig holds Microsoft Teams adapter settings.
type TeamsConfig struct {
	// AppID is the Microsoft Bot Framework App ID.
	AppID string `yaml:"app_id,omitempty" toml:"app_id,omitempty"`
	// AppPassword is the Microsoft Bot Framework App Password.
	AppPassword string `yaml:"app_password,omitempty" toml:"app_password,omitempty"`
	// ListenAddr is the address for incoming Bot Framework activities (e.g., ":3978").
	ListenAddr string `yaml:"listen_addr,omitempty" toml:"listen_addr,omitempty"`
}

// GoogleChatConfig holds Google Chat adapter settings.
// Google Chat uses the same logged-in user OAuth token as Gmail/Calendar/Drive;
// pass WithSecretProvider(sp) when calling InitMessenger so the adapter can resolve it.
type GoogleChatConfig struct{}

// WhatsAppConfig holds WhatsApp adapter settings.
type WhatsAppConfig struct {
}

// AGUIConfig holds AG-UI server and messenger adapter configuration.
// When AGUI is the active messenger (default), these settings configure both
// the in-process messenger adapter and the HTTP SSE server.
type AGUIConfig struct {
	// AppName is used as the session.Key AppName for thread tracking.
	// Defaults to "genie" if empty.
	AppName string `yaml:"app_name,omitempty" toml:"app_name,omitempty"`

	// --- Server settings ---
	CORSOrigins []string `yaml:"cors_origins,omitempty" toml:"cors_origins,omitempty"`
	Port        uint32   `yaml:"port,omitempty" toml:"port,omitempty"`
	// BindAddr is the listen address (e.g. ":9876" for all interfaces, "127.0.0.1:9876" for localhost only).
	// When empty, the server binds to ":port" so HTTP-push messengers (Teams, Google Chat, AGUI) are reachable from other hosts/containers.
	BindAddr      string  `yaml:"bind_addr,omitempty" toml:"bind_addr,omitempty"`
	RateLimit     float64 `yaml:"rate_limit,omitempty" toml:"rate_limit,omitempty"`         // req/sec per IP (0 = disabled)
	RateBurst     int     `yaml:"rate_burst,omitempty" toml:"rate_burst,omitempty"`         // burst allowance per IP
	MaxConcurrent int     `yaml:"max_concurrent,omitempty" toml:"max_concurrent,omitempty"` // max in-flight requests (0 = unlimited)
	MaxBodyBytes  int64   `yaml:"max_body_bytes,omitempty" toml:"max_body_bytes,omitempty"` // max request body in bytes (0 = unlimited)

	// Auth holds authentication settings (password, JWT/OIDC).
	// See auth.Config for all available options.
	Auth auth.Config `yaml:"auth,omitempty" toml:"auth,omitempty"`

	// AdminUsers is a list of user IDs (email, username, etc.) granted admin
	// privileges for destructive operations such as cache clearing.
	// When authentication is disabled (demo mode), all users have admin access.
	// Example: ["alice@company.com", "bob@company.com"]
	AdminUsers []string `yaml:"admin_users,omitempty" toml:"admin_users,omitempty"`
}

// DefaultAGUIConfig returns sensible defaults for the AG-UI server.
func DefaultAGUIConfig() AGUIConfig {
	return AGUIConfig{
		// Default to permissive CORS so local/dev UIs (including localhost and file://)
		// work out of the box. Tighten via config for production deployments.
		CORSOrigins:   []string{"*"},
		Port:          DefaultAGUIPort,
		RateLimit:     16.67, // 1000 req/min per IP
		RateBurst:     100,
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
// Pass WithSecretProvider(sp) when using Google Chat so it uses the logged-in user token.
func (c Config) InitMessenger(ctx context.Context, opts ...Option) (Messenger, error) {
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

	return c.newFromConfig(ctx, opts...)
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
		// Google Chat uses the logged-in user OAuth token (SecretProvider); no credentials_file.
	case PlatformWhatsApp:
		// WhatsApp uses QR code pairing at runtime; no token validation needed.
	case PlatformAGUI:
		// AGUI runs in-process; no external credentials required.
	}

	if len(errs) > 0 {
		return fmt.Errorf("messenger config validation: %s", strings.Join(errs, "; "))
	}
	return nil
}
