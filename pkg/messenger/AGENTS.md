# Messenger Package — Architecture & Constraints

## Purpose

Generic, bi-directional messaging abstraction for multi-platform communication (Slack, Discord, Telegram, etc.). This package is designed to be **open-sourceable** as a standalone library.

## Hard Constraints

> [!CAUTION]
> **ZERO coupling to genie, LLM, or agent code.** This package must NEVER import anything from `github.com/appcd-dev/genie` or any AI/LLM library. All dependencies must be Go standard library only (for the core interfaces). Platform adapter implementations may import their respective SDK (`slack-go/slack`, `bwmarrin/discordgo`, `go-telegram/bot`).

## Design Principles

1. **2-Way Communication**: The `Messenger` interface supports both sending and receiving messages. Outbound-only notification patterns (like `shoutrrr` or `nikoksr/notify`) are insufficient for our use case.
2. **Adapter Pattern**: Each platform implements the `Messenger` interface. Adapters live in sub-packages (e.g., `messenger/slack`, `messenger/discord`).
3. **Pure Communication Structs**: All types (`SendRequest`, `IncomingMessage`, `Channel`, etc.) describe messaging concepts only — no tool calls, no agent state, no LLM events.
4. **Context-Driven Lifecycle**: `Connect`, `Disconnect`, `Send`, and `Receive` all take `context.Context` for cancellation and timeout control.
5. **Thread-Aware**: Messages carry `ThreadID` for threaded conversations (Slack threads, Discord threads, Telegram reply chains).

## Interface

```go
type Messenger interface {
    Connect(ctx context.Context) error
    Disconnect(ctx context.Context) error
    Send(ctx context.Context, req SendRequest) (SendResponse, error)
    Receive(ctx context.Context) (<-chan IncomingMessage, error)
    Platform() Platform
}
```

## Implemented Adapters

| Platform | Sub-package | Library | Transport |
|----------|-------------|---------|-----------|
| Slack | `messenger/slack` | `github.com/slack-go/slack` | Socket Mode (no public endpoint) |
| Discord | `messenger/discord` | `github.com/bwmarrin/discordgo` | WebSocket gateway |
| Telegram | `messenger/telegram` | `github.com/go-telegram/bot` | Long-polling |
| Teams | `messenger/teams` | `github.com/infracloudio/msbotbuilder-go` | Bot Framework (HTTP) |
| Google Chat | `messenger/googlechat` | `google.golang.org/api/chat/v1` | HTTP push + Chat API |
| WhatsApp | `messenger/whatsapp` | `go.mau.fi/whatsmeow` | WhatsApp Web (multi-device) |

## Future Adapters

| Platform | Library | Transport |
|----------|---------|-----------|
| Notifications (outbound-only) | `github.com/containrrr/shoutrrr` | Fire-and-forget |

## Maintenance Rules

> [!IMPORTANT]
> When adding or removing a messaging provider, you **must** also update the following files to keep the docs site in sync:
> - `docs/gh-pages/index.html` — platform mentions in feature cards
> - `docs/gh-pages/config-builder.html` — messenger section description
> - `docs/gh-pages/js/config-builder.js` — state defaults, PLATFORMS array, form renderer, TOML serializer, YAML serializer

## Testing

- Ginkgo/Gomega following repo conventions
- `counterfeiter` for generating fakes of the `Messenger` interface
- Contract tests validate the interface lifecycle (connect → send/receive → disconnect)

## Prior Art

Interface design inspired by:
- [`target/flottbot`](https://github.com/target/flottbot) — `Remote` interface with `Read()`/`Send()`/`Reaction()`
- [`binwiederhier/replbot`](https://github.com/binwiederhier/replbot) — `conn` interface abstracting Slack + Discord
