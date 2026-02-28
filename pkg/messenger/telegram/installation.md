# Telegram — Installation & Configuration Guide

This guide walks you through connecting Genie to Telegram using **long-polling** (no public endpoint required).

---

## Prerequisites

- A Telegram account.
- Access to your Genie configuration file (`.genie.toml`).

---

## Step 1: Create a Bot via BotFather

1. Open Telegram and search for **@BotFather** (or navigate to [https://t.me/BotFather](https://t.me/BotFather)).
2. Start a conversation and send the command:

   ```
   /newbot
   ```

3. Follow the prompts:
   - Enter a **display name** for your bot (e.g., `Genie Assistant`).
   - Enter a **username** for your bot — it must end in `bot` (e.g., `genie_assistant_bot`).
4. BotFather will respond with your **Bot API Token**. Copy it — this is your `token`.

   Example format: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`

> **Important:** Keep your token secret. Anyone with this token can control your bot.

---

## Step 2: Configure Bot Settings (Optional)

You can customize your bot via BotFather commands:

| Command | Purpose |
|---|---|
| `/setdescription` | Set the bot's description shown on its profile |
| `/setabouttext` | Set the "About" text |
| `/setuserpic` | Set the bot's profile picture |
| `/setcommands` | Define command hints for users |

### Group Privacy Mode

By default, Telegram bots in **group chats** only receive messages that mention the bot or start with a `/` command. To allow the bot to read all messages in groups:

1. Send `/mybots` to BotFather.
2. Select your bot → **Bot Settings** → **Group Privacy**.
3. Set to **Disabled** (the bot will receive all group messages).

> **Note:** For direct messages (1:1 chats), the bot always receives all messages regardless of this setting.

---

## Step 3: Configure Genie

Add the following to your `.genie.toml` configuration file:

```toml
[messenger]
platform = "telegram"

[messenger.telegram]
token = "123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
```

> **Security:** Never commit your bot token to version control. Use environment variables or a secrets manager in production.

---

## Step 4: Start a Conversation

1. Search for your bot by its username in Telegram (e.g., `@genie_assistant_bot`).
2. Click **Start** to begin a conversation.
3. Send a message to verify the bot responds.

For group chats:

1. Add the bot to a group by opening the group → **Add Members** → search for your bot.
2. If Group Privacy is enabled (default), mention the bot or use `/` commands.

---

## Verification

After starting Genie, you should see the log message:

```
connected to Telegram via long-polling
```

Send a message to the bot to confirm everything is working.

---

## Troubleshooting

| Issue | Solution |
|---|---|
| `failed to create telegram bot` | Verify the `token` is correct. Regenerate via `/token` in BotFather if needed. |
| Bot doesn't respond in groups | Check **Group Privacy** setting — disable it if the bot should read all messages. |
| Bot responds slowly | Long-polling has a slight delay compared to webhooks. This is normal. |
| Duplicate bot responses | Ensure only **one** instance of Genie is running with the same bot token. |

---

## Required Tokens Summary

| Token | Where to Find |
|---|---|
| `token` | BotFather → `/newbot` or `/mybots` → API Token |
