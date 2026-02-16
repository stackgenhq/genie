# Messaging Configuration

Genie supports sending messages to external chat platforms via the `pkg/messenger` package. When configured, the **ReAcTree agent** gains a `send_message` tool that can post updates, alerts, or results to Slack, Telegram, Microsoft Teams, or Google Chat.

## Quick Start

Add a `[messenger]` section to your `.genie.toml`:

```toml
[messenger]
platform = "slack"
buffer_size = 100

[messenger.slack]
app_token = "${SLACK_APP_TOKEN}"
bot_token = "${SLACK_BOT_TOKEN}"
```

The agent will automatically have access to a `send_message` tool during ReAcTree execution.

## Supported Platforms

### Slack

Uses **Socket Mode** — no public endpoint required.

```toml
[messenger]
platform = "slack"

[messenger.slack]
app_token = "${SLACK_APP_TOKEN}"   # xapp-... (App-Level Token)
bot_token = "${SLACK_BOT_TOKEN}"   # xoxb-... (Bot User OAuth Token)
```

**Setup:**
1. Create a Slack App at [api.slack.com/apps](https://api.slack.com/apps)
2. Enable Socket Mode and generate an App-Level Token
3. Add bot scopes: `chat:write`, `channels:read`, `channels:history`
4. Install the app to your workspace

### Telegram

Uses **long-polling** — no webhook endpoint required.

```toml
[messenger]
platform = "telegram"

[messenger.telegram]
token = "${TELEGRAM_BOT_TOKEN}"    # From @BotFather
```

**Setup:**
1. Message [@BotFather](https://t.me/BotFather) on Telegram
2. Create a new bot with `/newbot`
3. Copy the token

### Microsoft Teams

Uses the **Bot Framework** — requires an HTTP endpoint for incoming activities.

```toml
[messenger]
platform = "teams"

[messenger.teams]
app_id = "${TEAMS_APP_ID}"
app_password = "${TEAMS_APP_PASSWORD}"
listen_addr = ":3978"              # Default: :3978
```

**Setup:**
1. Register a bot in the [Azure Bot Service](https://portal.azure.com)
2. Note the App ID and generate a password
3. Configure the messaging endpoint to point to your server's `/api/messages`

### Google Chat

Uses **HTTP push** for incoming events and the **Chat API** for outgoing messages.

```toml
[messenger]
platform = "googlechat"

[messenger.googlechat]
credentials_file = "/path/to/service-account.json"
listen_addr = ":8080"              # Endpoint for incoming events
```

**Setup:**
1. Create a Google Cloud project and enable the Chat API
2. Create a service account and download the JSON key
3. Configure your Chat app in the [Google Chat API console](https://console.cloud.google.com/apis/api/chat.googleapis.com)
4. Set the app URL to your server's address

## Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `messenger.platform` | string | `""` (disabled) | Platform to use: `slack`, `telegram`, `teams`, `googlechat` |
| `messenger.buffer_size` | int | `100` | Incoming message channel buffer size |
| `messenger.slack.app_token` | string | — | Slack App-Level Token for Socket Mode |
| `messenger.slack.bot_token` | string | — | Slack Bot User OAuth Token |
| `messenger.telegram.token` | string | — | Telegram Bot API token |
| `messenger.teams.app_id` | string | — | Microsoft Bot Framework App ID |
| `messenger.teams.app_password` | string | — | Microsoft Bot Framework App Password |
| `messenger.teams.listen_addr` | string | `:3978` | HTTP listen address for Bot Framework |
| `messenger.googlechat.credentials_file` | string | — | Path to Google service account JSON key |
| `messenger.googlechat.listen_addr` | string | — | HTTP listen address for push events |

## Environment Variables

All config values support `${ENV_VAR}` expansion. Common variables:

| Variable | Used by |
|----------|---------|
| `SLACK_APP_TOKEN` | Slack adapter |
| `SLACK_BOT_TOKEN` | Slack adapter |
| `TELEGRAM_BOT_TOKEN` | Telegram adapter |
| `TEAMS_APP_ID` | Teams adapter |
| `TEAMS_APP_PASSWORD` | Teams adapter |

## How It Works

```
.genie.toml ──[messenger]──► messenger.NewFromConfig()
                                    │
                                    ▼
                            messenger.Messenger
                                    │
                        ┌───────────┼───────────┐
                        ▼           ▼           ▼
                  ToolDeps      Connect()    Receive()
                      │                        │
                      ▼                        ▼
                send_message tool     ┌──────────────────┐
                (ReAcTree agent)      │  grantWithTUI    │
                                      │  select loop     │
                   TUI ──inputChan──► │                  │──► codeOwner.Chat()
                   TCP ──UserInputMsg─┤                  │──► tui.EmitAgentMessage
                   Messenger ─────────┘                  │──► msgr.Send() (reply)
                                      └──────────────────┘
```

1. Config is loaded from `.genie.toml` via `LoadGenieConfig()`
2. If `messenger.platform` is set, `NewFromConfig()` creates the adapter
3. The adapter is connected and injected into `codeowner.ToolDeps`
4. The ReAcTree agent receives a `send_message` tool (outbound only)
5. `grantWithTUI` calls `messenger.Receive()` and multiplexes incoming messages alongside TUI and TCP input
6. Messenger replies are sent back to the originating channel/thread
7. On shutdown, the adapter is gracefully disconnected

## Programmatic Usage

```go
import (
    "github.com/appcd-dev/genie/pkg/messenger"
    _ "github.com/appcd-dev/genie/pkg/messenger/slack"  // register adapter
)

// Create from config
m, err := messenger.NewFromConfig(cfg.Messenger)

// Or create directly
m := slack.New(slack.Config{
    AppToken: os.Getenv("SLACK_APP_TOKEN"),
    BotToken: os.Getenv("SLACK_BOT_TOKEN"),
})

// Connect and send
m.Connect(ctx)
defer m.Disconnect(ctx)

m.Send(ctx, messenger.SendRequest{
    Channel: messenger.Channel{ID: "C1234567890"},
    Content: messenger.MessageContent{Text: "Hello from Genie!"},
})
```

## Known Limitations & Blindspots

> [!NOTE]
> Items marked ✅ have been addressed. Items marked ⚠️ are known but not yet fixed.

### ✅ 1. Sequential Message Handling

Messenger messages are now dispatched in goroutines, so slow LLM calls don't block the select loop. TUI input remains sequential (expected UX).

### ✅ 2. No Sender Context

`CodeQuestion.SenderContext` now carries sender identity (e.g. `slack:U12345:C67890`), injected into the prompt so the LLM knows who is asking and from which platform.

### ✅ 3. Duplicate Replies

When a message originates from a messenger, the `send_message` tool is excluded via `CodeQuestion.ExcludeTools` since the chat loop handles the reply directly. This prevents double-posting.

### ✅ 4. Reconnection on Disconnect

`ReceiveWithReconnect()` in `pkg/messenger/reconnect.go` wraps `Receive()` in a goroutine with exponential backoff (1s → 30s). When the platform drops the connection, it automatically retries and forwards messages through a relay channel that stays open until context cancellation.

### ✅ 5. Loading Indicator for TCP Input

The `UserInputMsg` handler in `model.go` now calls `SetLoading(true)` after forwarding input, so the TUI shows a "Genie is thinking..." spinner for TCP/connect users — matching the keyboard input UX.

### ✅ 6. Source Attribution in TUI

`UserInputMsg` now carries a `Source` field, and `ChatMessage` stores the `Sender`. TCP input shows as "user (connect)" and messenger input shows sender name + platform (e.g. "Alice (slack)"). A styled attribution label renders above external user bubbles.

### ✅ 7. Thread-Isolated Memory

`UserKey.UserID` now varies per sender/thread (derived from `SenderContext`), so conversation history is isolated across Slack threads, TUI sessions, and different users.

### ⚠️ 8. No Rate Limiting or Access Control

Anyone who can message the bot on Slack/Teams can trigger `codeOwner.Chat()` and LLM calls. There's no auth/rate-limit layer between `Receive()` and the expert — unlike the TCP listener which has mTLS + audit logging.

