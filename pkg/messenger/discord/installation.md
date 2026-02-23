# Discord — Installation & Configuration Guide

This guide walks you through connecting Genie to your Discord server using the **WebSocket gateway** (no public endpoint required).

---

## Prerequisites

- A Discord server where you have **Manage Server** permissions.
- Access to your Genie configuration file (`.genie.toml`).

---

## Step 1: Create a Discord Application

1. Go to [https://discord.com/developers/applications](https://discord.com/developers/applications).
2. Click **New Application**.
3. Enter an **Application Name** (e.g., `Genie Bot`) and click **Create**.

---

## Step 2: Create a Bot User

1. In your application settings, navigate to **Bot** in the left sidebar.
2. Click **Add Bot** → **Yes, do it!**
3. Under the bot settings:
   - Optionally set a **username** and **avatar** for the bot.
   - Under **Token**, click **Reset Token** to generate a new token.
   - Copy the token — this is your `bot_token`.

> **Important:** The token is shown only once. Store it securely.

---

## Step 3: Configure Privileged Gateway Intents

Genie requires access to message content to function properly.

1. On the **Bot** settings page, scroll down to **Privileged Gateway Intents**.
2. Enable the following intents:

| Intent | Purpose |
|---|---|
| **Message Content Intent** | Read the content of messages sent in servers |
| **Server Members Intent** | *(Optional)* Access to member information |

3. Click **Save Changes**.

> **Note:** If your bot is in more than 100 servers, these intents require Discord verification.

---

## Step 4: Generate an Invite URL

1. Navigate to **OAuth2 → URL Generator** in the left sidebar.
2. Under **Scopes**, select:
   - `bot`
3. Under **Bot Permissions**, select:
   - `Send Messages`
   - `Read Message History`
   - `View Channels`
   - `Embed Links` *(optional, for rich responses)*
   - `Attach Files` *(optional, for file sharing)*
4. Copy the generated **URL** at the bottom of the page.

---

## Step 5: Invite the Bot to Your Server

1. Open the invite URL from Step 4 in your browser.
2. Select the **server** you want to add the bot to.
3. Click **Authorize** and complete the CAPTCHA.

---

## Step 6: Configure Genie

Add the following to your `.genie.toml` configuration file:

```toml
[messenger]
platform = "discord"

[messenger.discord]
bot_token = "MTIzNDU2Nzg5MDEyMzQ1Njc4OQ.XXXXXX.XXXXXXXXXXXXXXXXXXXXXXXXXXXX"
```

> **Security:** Never commit your bot token to version control. Use environment variables or a secrets manager in production.

---

## Verification

After starting Genie, you should see the log message:

```
connected to Discord via WebSocket gateway
```

Send a message in a channel where the bot is present, or send a direct message to the bot to confirm everything is working.

---

## Troubleshooting

| Issue | Solution |
|---|---|
| `failed to create discord session` | Verify the `bot_token` is correct and not expired. Regenerate if needed. |
| Bot is online but doesn't respond | Ensure **Message Content Intent** is enabled in the Developer Portal. |
| Bot can't see messages in a channel | Check that the bot has **View Channel** and **Read Message History** permissions in that channel. |
| `incoming message buffer full` | Increase the `buffer_size` in your messenger config or ensure messages are being consumed. |

---

## Required Tokens Summary

| Token | Where to Find |
|---|---|
| `bot_token` | Discord Developer Portal → Your App → Bot → Token |
