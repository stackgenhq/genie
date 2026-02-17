// Package whatsapp provides a Messenger adapter for WhatsApp using the
// WhatsApp Web multi-device protocol via whatsmeow.
//
// The adapter connects to WhatsApp as a linked device (the same mechanism as
// WhatsApp Web). On first use it displays a QR code in the terminal for
// pairing. Subsequent connections reuse stored credentials automatically.
//
// Transport: WhatsApp Web WebSocket (no public endpoint, no Meta Business API).
//
// # Authentication
//
// No pre-configured tokens are required. Pair once by scanning a QR code with
// the phone running WhatsApp. Credentials are persisted in a local SQLite
// database (default: ~/.genie/whatsapp/whatsmeow.db).
//
// # Usage
//
//	m, err := whatsapp.New(whatsapp.Config{
//		StorePath: "~/.genie/whatsapp",
//	})
//	if err != nil { /* handle */ }
//	if err := m.Connect(ctx); err != nil { /* handle */ }
//	defer m.Disconnect(ctx)
package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	waLog "go.mau.fi/whatsmeow/util/log"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver for whatsmeow store.
)

func init() {
	messenger.RegisterAdapter(messenger.PlatformWhatsApp, func(params map[string]string, opts ...messenger.Option) (messenger.Messenger, error) {
		cfg := Config{
			StorePath: params["store_path"],
		}
		return New(cfg, opts...)
	})
}

// DefaultStorePath is the default directory for whatsmeow session storage.
const DefaultStorePath = "~/.genie/whatsapp"

// Config holds WhatsApp adapter configuration.
type Config struct {
	// StorePath is the directory for whatsmeow session/credential storage.
	// Defaults to DefaultStorePath if empty.
	StorePath string
}

// Messenger implements the [messenger.Messenger] interface for WhatsApp
// using the whatsmeow library (WhatsApp Web multi-device protocol).
type Messenger struct {
	cfg        Config
	adapterCfg messenger.AdapterConfig
	client     *whatsmeow.Client
	container  *sqlstore.Container
	incoming   chan messenger.IncomingMessage
	connected  bool
	cancel     context.CancelFunc
	mu         sync.RWMutex
}

// New creates a new WhatsApp Messenger. The store path defaults to
// ~/.genie/whatsapp if not set in the config.
func New(cfg Config, opts ...messenger.Option) (*Messenger, error) {
	if cfg.StorePath == "" {
		cfg.StorePath = DefaultStorePath
	}

	// Expand ~ to home directory.
	if len(cfg.StorePath) > 0 && cfg.StorePath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("whatsapp: failed to resolve home dir: %w", err)
		}
		cfg.StorePath = filepath.Join(home, cfg.StorePath[1:])
	}

	adapterCfg := messenger.ApplyOptions(opts...)
	return &Messenger{
		cfg:        cfg,
		adapterCfg: adapterCfg,
	}, nil
}

// Connect establishes a connection to WhatsApp via the Web multi-device
// protocol. If no stored session exists, it prints a QR code to the
// terminal for pairing.
func (m *Messenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "whatsapp", "fn", "whatsapp.Connect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.connected {
		return messenger.ErrAlreadyConnected
	}

	// Ensure store directory exists.
	if err := os.MkdirAll(m.cfg.StorePath, 0o700); err != nil {
		return fmt.Errorf("whatsapp: failed to create store dir %s: %w", m.cfg.StorePath, err)
	}

	// Initialize SQLite-backed device store.
	dbPath := filepath.Join(m.cfg.StorePath, "whatsmeow.db")
	dbURI := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", dbPath)
	container, err := sqlstore.New(ctx, "sqlite", dbURI, waLog.Noop)
	if err != nil {
		return fmt.Errorf("whatsapp: failed to open store: %w", err)
	}
	m.container = container

	// Get or create the device store.
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: failed to get device store: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Noop)
	m.client = client

	m.incoming = make(chan messenger.IncomingMessage, m.adapterCfg.MessageBufferSize)

	// Register event handler for incoming messages.
	client.AddEventHandler(m.eventHandler)

	// Connect — if device is not yet linked, do QR pairing.
	if client.Store.ID == nil {
		log.Info("no stored WhatsApp session found, starting QR code pairing")
		log.Info("scan the QR code below with your WhatsApp app (Settings → Linked Devices → Link a Device)")

		qrChan, _ := client.GetQRChannel(ctx)
		if err := client.Connect(); err != nil {
			return fmt.Errorf("whatsapp: failed to connect for QR pairing: %w", err)
		}

		// Wait for QR code events.
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				// Print QR code to terminal.
				fmt.Println("\n╔══════════════════════════════════════╗")
				fmt.Println("║   Scan this QR code with WhatsApp   ║")
				fmt.Println("╚══════════════════════════════════════╝")
				fmt.Println()
				fmt.Println(evt.Code)
				fmt.Println()
				fmt.Println("Open WhatsApp → Settings → Linked Devices → Link a Device")
				fmt.Println()
			case "success":
				log.Info("WhatsApp QR pairing successful")
			case "timeout":
				return fmt.Errorf("whatsapp: QR code scan timed out — please restart and try again")
			}
		}
	} else {
		log.Info("reconnecting to WhatsApp with stored session")
		if err := client.Connect(); err != nil {
			return fmt.Errorf("whatsapp: failed to connect: %w", err)
		}
	}

	_, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	m.connected = true
	log.Info("connected to WhatsApp via Web protocol")
	return nil
}

// Disconnect gracefully shuts down the WhatsApp connection.
func (m *Messenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("platform", "whatsapp", "fn", "whatsapp.Disconnect")
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.connected {
		return messenger.ErrNotConnected
	}

	m.cancel()

	if m.client != nil {
		m.client.Disconnect()
	}

	close(m.incoming)
	m.connected = false
	log.Info("disconnected from WhatsApp")
	return nil
}

// Send delivers a message to a WhatsApp conversation.
func (m *Messenger) Send(ctx context.Context, req messenger.SendRequest) (messenger.SendResponse, error) {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()

	if !connected {
		return messenger.SendResponse{}, messenger.ErrNotConnected
	}

	// Parse the recipient JID from the channel ID.
	// Channel ID should be a phone number in E.164 format (without +).
	jid := types.NewJID(req.Channel.ID, types.DefaultUserServer)

	msg := &waE2E.Message{
		Conversation: stringPtr(req.Content.Text),
	}

	resp, err := m.client.SendMessage(ctx, jid, msg)
	if err != nil {
		return messenger.SendResponse{}, fmt.Errorf("%w: %s", messenger.ErrSendFailed, err)
	}

	return messenger.SendResponse{
		MessageID: resp.ID,
		Timestamp: resp.Timestamp,
	}, nil
}

// Receive returns a channel of incoming messages from WhatsApp.
func (m *Messenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.connected {
		return nil, messenger.ErrNotConnected
	}

	return m.incoming, nil
}

// Platform returns the WhatsApp platform identifier.
func (m *Messenger) Platform() messenger.Platform {
	return messenger.PlatformWhatsApp
}

// eventHandler processes whatsmeow events and publishes incoming messages.
func (m *Messenger) eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		m.handleMessage(v)
	}
}

// handleMessage converts a whatsmeow message event to an IncomingMessage.
func (m *Messenger) handleMessage(evt *events.Message) {
	// Skip messages sent by us.
	if evt.Info.IsFromMe {
		return
	}

	// Extract text content.
	text := ""
	if evt.Message.GetConversation() != "" {
		text = evt.Message.GetConversation()
	} else if evt.Message.GetExtendedTextMessage() != nil {
		text = evt.Message.GetExtendedTextMessage().GetText()
	}

	// Skip non-text messages for now.
	if text == "" {
		return
	}

	// Determine channel type.
	channelType := messenger.ChannelTypeDM
	channelID := evt.Info.Sender.User
	channelName := ""
	if evt.Info.IsGroup {
		channelType = messenger.ChannelTypeGroup
		channelID = evt.Info.Chat.User
		channelName = channelID
	}

	incoming := messenger.IncomingMessage{
		ID:       evt.Info.ID,
		Platform: messenger.PlatformWhatsApp,
		Channel: messenger.Channel{
			ID:   channelID,
			Name: channelName,
			Type: channelType,
		},
		Sender: messenger.Sender{
			ID:          evt.Info.Sender.User,
			Username:    evt.Info.Sender.User,
			DisplayName: evt.Info.PushName,
		},
		Content: messenger.MessageContent{
			Text: text,
		},
		Timestamp: evt.Info.Timestamp,
	}

	select {
	case m.incoming <- incoming:
	default:
		// Buffer full — drop message rather than blocking.
	}
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}

// FormatApproval returns the request unchanged — WhatsApp does not use
// rich card formatting for approval notifications.
func (m *Messenger) FormatApproval(req messenger.SendRequest, _ messenger.ApprovalInfo) messenger.SendRequest {
	return req
}

// Compile-time interface compliance check.
var _ messenger.Messenger = (*Messenger)(nil)
