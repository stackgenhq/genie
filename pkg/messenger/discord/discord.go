// Package discord provides a Messenger adapter for the Discord platform
// using the WebSocket gateway for bi-directional communication.
//
// The adapter wraps [github.com/bwmarrin/discordgo] and converts
// Discord-native events into the generic [messenger.IncomingMessage] type.
// Outgoing messages are sent via the Discord REST API.
//
// Transport: WebSocket gateway (no public endpoint required).
//
// # Authentication
//
// A Discord bot token is required. Create one via the Discord Developer Portal
// and pass it via [Config.BotToken].
//
// # Usage
//
//	m, err := discord.New(discord.Config{BotToken: os.Getenv("DISCORD_BOT_TOKEN")})
//	if err != nil { /* handle */ }
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package discord

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/go-lib/logger"
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformDiscord, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		return New(Config{
			BotToken: params["bot_token"],
		}, opts...)
	})
}

// Config holds Discord-specific configuration.
type Config struct {
	// BotToken is the Discord bot token from the Developer Portal.
	BotToken string
}

// Messenger implements the [messenger.Messenger] interface for Discord
// using the WebSocket gateway. It manages the session lifecycle, incoming
// message buffer, and connection state through an internal mutex.
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	session    *discordgo.Session
	incoming   chan messenger.IncomingMessage
	connected  bool
	connCtx    context.Context
	mu         sync.RWMutex
}

// New creates a new Discord Messenger with the given config and options.
// Returns an error if the underlying Discord session cannot be initialised
// (e.g., malformed token).
func New(cfg Config, opts ...messenger.Option) (*Messenger, error) {
	adapterCfg := messenger.ApplyOptions(opts...)

	session, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	// We only need message events.
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages

	m := &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
		session:    session,
	}

	// Register the message handler before connecting.
	session.AddHandler(m.messageCreate)

	return m, nil
}

// Connect opens the WebSocket gateway connection to Discord.
func (m *Messenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "discord", "fn", "discord.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return messenger.ErrAlreadyConnected
	}

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)
	m.connCtx = ctx

	if err := m.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	m.connected = true
	log.Info("connected to Discord via WebSocket gateway")
	return nil
}

// Disconnect gracefully shuts down the Discord session.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "discord", "fn", "discord.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	if err := m.session.Close(); err != nil {
		log.Error("discord session close error", "error", err)
	}

	close(m.incoming)
	m.connected = false
	log.Info("disconnected from Discord")
	return nil
}

// Send delivers a message to a Discord channel or thread.
func (m *Messenger) Send(_ context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	var (
		msg *discordgo.Message
		err error
	)

	if req.ThreadID != "" {
		// ThreadID in Discord is a channel ID of the thread.
		msg, err = m.session.ChannelMessageSend(req.ThreadID, req.Content.Text)
	} else {
		msg, err = m.session.ChannelMessageSend(req.Channel.ID, req.Content.Text)
	}

	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
	}

	return messenger.SendResponse{
		MessageID: msg.ID,
		Timestamp: msg.Timestamp,
	}, nil
}

// Receive returns a channel of incoming messages from Discord.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the Discord platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformDiscord
}

// messageCreate is the discordgo event handler that converts Discord messages
// to IncomingMessage and publishes them to the incoming channel.
func (m *Messenger) messageCreate(_ *discordgo.Session, event *discordgo.MessageCreate) {
	// Skip messages from the bot itself to avoid echo loops.
	if event.Author.ID == m.session.State.User.ID {
		return
	}

	// Determine channel type.
	channelType := messenger.ChannelTypeChannel
	ch, err := m.session.State.Channel(event.ChannelID)
	if err == nil {
		switch ch.Type {
		case discordgo.ChannelTypeDM:
			channelType = messenger.ChannelTypeDM
		case discordgo.ChannelTypeGroupDM:
			channelType = messenger.ChannelTypeGroup
		}
	}

	// Determine thread ID — if the message is in a thread, use the channel ID
	// of the thread itself.
	threadID := ""
	if ch != nil && ch.IsThread() {
		threadID = ch.ID
	}

	channelName := ""
	if ch != nil {
		channelName = ch.Name
	}

	incoming := messenger.IncomingMessage{
		ID:       event.ID,
		Platform: messenger.PlatformDiscord,
		Channel: messenger.Channel{
			ID:   event.ChannelID,
			Name: channelName,
			Type: channelType,
		},
		Sender: messenger.Sender{
			ID:          event.Author.ID,
			Username:    event.Author.Username,
			DisplayName: event.Author.GlobalName,
		},
		Content: messenger.MessageContent{
			Text: event.Content,
		},
		ThreadID:  threadID,
		Timestamp: time.Now(),
	}

	select {
	case m.incoming <- incoming:
	default:
		logger.GetLogger(m.connCtx).With("platform", "discord").Warn("incoming message buffer full, dropping message",
			"channel_id", event.ChannelID, "user_id", event.Author.ID)
	}
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
