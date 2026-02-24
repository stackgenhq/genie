package messenger

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/logger"
)

// NewFromConfig creates a Messenger from a Config, selecting the appropriate
// adapter based on the Platform field. Returns an error if the platform is
// unknown or the required config fields are missing.
func (cfg Config) newFromConfig(ctx context.Context, opts ...Option) (Messenger, error) {
	if !cfg.enabled() {
		return nil, fmt.Errorf("messenger: no platform configured")
	}

	// Apply buffer size from config if set and not overridden by options.
	if cfg.BufferSize > 0 {
		opts = append([]Option{WithMessageBuffer(cfg.BufferSize)}, opts...)
	}

	logger.GetLogger(ctx).Info("Initializing Messenger", "platform", cfg.Platform)
	switch cfg.Platform {
	case PlatformSlack:
		return newSlackFromConfig(cfg.Slack, opts...)
	case PlatformDiscord:
		return newDiscordFromConfig(cfg.Discord, opts...)
	case PlatformTelegram:
		return newTelegramFromConfig(cfg.Telegram, opts...)
	case PlatformTeams:
		return newTeamsFromConfig(cfg.Teams, opts...)
	case PlatformGoogleChat:
		return newGoogleChatFromConfig(cfg.GoogleChat, opts...)
	case PlatformWhatsApp:
		return newWhatsAppFromConfig(cfg.WhatsApp, opts...)
	case PlatformAGUI:
		return newAGUIFromConfig(opts...)
	default:
		return nil, fmt.Errorf("messenger: unsupported platform %q", cfg.Platform)
	}
}

// newSlackFromConfig validates Slack config and creates a Slack messenger.
// Uses lazy import pattern to avoid importing adapter packages at the
// interface level — callers must register adapters or use direct imports.
func newSlackFromConfig(cfg SlackConfig, opts ...Option) (Messenger, error) {
	if cfg.AppToken == "" || cfg.BotToken == "" {
		return nil, fmt.Errorf("messenger: slack requires app_token and bot_token")
	}
	// Factory returns a deferred-init messenger; actual adapter creation
	// happens via the adapter sub-package imported by the application binary.
	f, ok := adapterFactories[PlatformSlack]
	if !ok {
		return nil, fmt.Errorf("messenger: slack adapter not registered (import _ \"...messenger/slack\")")
	}
	return f(map[string]string{
		"app_token": cfg.AppToken,
		"bot_token": cfg.BotToken,
	}, opts...)
}

func newDiscordFromConfig(cfg DiscordConfig, opts ...Option) (Messenger, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("messenger: discord requires bot_token")
	}
	f, ok := adapterFactories[PlatformDiscord]
	if !ok {
		return nil, fmt.Errorf("messenger: discord adapter not registered (import _ \"...messenger/discord\")")
	}
	return f(map[string]string{
		"bot_token": cfg.BotToken,
	}, opts...)
}

func newTelegramFromConfig(cfg TelegramConfig, opts ...Option) (Messenger, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("messenger: telegram requires token")
	}
	f, ok := adapterFactories[PlatformTelegram]
	if !ok {
		return nil, fmt.Errorf("messenger: telegram adapter not registered (import _ \"...messenger/telegram\")")
	}
	return f(map[string]string{
		"token": cfg.Token,
	}, opts...)
}

func newTeamsFromConfig(cfg TeamsConfig, opts ...Option) (Messenger, error) {
	if cfg.AppID == "" || cfg.AppPassword == "" {
		return nil, fmt.Errorf("messenger: teams requires app_id and app_password")
	}
	f, ok := adapterFactories[PlatformTeams]
	if !ok {
		return nil, fmt.Errorf("messenger: teams adapter not registered (import _ \"...messenger/teams\")")
	}
	params := map[string]string{
		"app_id":       cfg.AppID,
		"app_password": cfg.AppPassword,
	}
	if cfg.ListenAddr != "" {
		params["listen_addr"] = cfg.ListenAddr
	}
	return f(params, opts...)
}

func newGoogleChatFromConfig(cfg GoogleChatConfig, opts ...Option) (Messenger, error) {
	f, ok := adapterFactories[PlatformGoogleChat]
	if !ok {
		return nil, fmt.Errorf("messenger: googlechat adapter not registered (import _ \"...messenger/googlechat\")")
	}
	params := map[string]string{}
	if cfg.CredentialsFile != "" {
		params["credentials_file"] = cfg.CredentialsFile
	}
	if cfg.ListenAddr != "" {
		params["listen_addr"] = cfg.ListenAddr
	}
	return f(params, opts...)
}

func newWhatsAppFromConfig(cfg WhatsAppConfig, opts ...Option) (Messenger, error) {
	f, ok := adapterFactories[PlatformWhatsApp]
	if !ok {
		return nil, fmt.Errorf("messenger: whatsapp adapter not registered (import _ \"...messenger/whatsapp\")")
	}
	params := map[string]string{}
	if cfg.StorePath != "" {
		params["store_path"] = cfg.StorePath
	}
	return f(params, opts...)
}

func newAGUIFromConfig(opts ...Option) (Messenger, error) {
	f, ok := adapterFactories[PlatformAGUI]
	if !ok {
		return nil, fmt.Errorf("messenger: agui adapter not registered (import _ \"...messenger/agui\")")
	}
	return f(map[string]string{}, opts...)
}

// AdapterFactory creates a Messenger from generic string params.
// Used by the registry pattern so the parent package doesn't import adapters.
type AdapterFactory func(params map[string]string, opts ...Option) (Messenger, error)

// adapterFactories is the global registry of adapter factories.
// Adapter sub-packages register themselves via init().
var adapterFactories = map[Platform]AdapterFactory{}

// RegisterAdapter registers an adapter factory for a given platform.
// Called by adapter sub-packages in their init() functions.
func RegisterAdapter(platform Platform, factory AdapterFactory) {
	adapterFactories[platform] = factory
	logger.GetLogger(context.Background()).Debug("Registered messenger adapter", "platform", platform)
}
