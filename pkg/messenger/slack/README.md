# Slack — Installation & Configuration Guide

This guide walks you through connecting Genie to your Slack workspace using **Socket Mode** (WebSocket-based — no public endpoint required).

---

## Prerequisites

- A Slack workspace where you have **admin** or **app-management** permissions.
- Access to your Genie configuration file (`.genie.toml`).

---

## Step 1: Create a Slack App

1. Go to [https://api.slack.com/apps](https://api.slack.com/apps) and click **Create New App**.
2. Choose **From scratch**.
3. Enter an **App Name** (e.g., `Genie Bot`) and select your **workspace**.
4. Click **Create App**.

---

## Step 2: Enable Socket Mode

Socket Mode lets your app receive events over a WebSocket connection instead of requiring a public URL.

1. In your app settings, navigate to **Settings → Socket Mode**.
2. Toggle **Enable Socket Mode** to **On**.
3. You will be prompted to generate an **App-Level Token**:
   - Give it a name (e.g., `genie-socket`).
   - Add the scope: **`connections:write`**.
   - Click **Generate**.
4. Copy the generated token — it starts with **`xapp-`**. This is your `app_token`.

---

## Step 3: Configure Bot Token Scopes

1. Navigate to **Features → OAuth & Permissions**.
2. Under **Bot Token Scopes**, add the following scopes:

| Scope | Purpose |
|---|---|
| `chat:write` | Send messages as the bot |
| `channels:history` | Read messages in public channels |
| `groups:history` | Read messages in private channels |
| `im:history` | Read direct messages |
| `mpim:history` | Read group direct messages |
| `users:read` | Look up user information |

> **Tip:** Add additional scopes as needed for your use case (e.g., `files:read` for file access).

---

## Step 4: Subscribe to Events

1. Navigate to **Features → Event Subscriptions**.
2. Toggle **Enable Events** to **On**.
3. Under **Subscribe to bot events**, add:

| Event | Description |
|---|---|
| `message.channels` | Messages in public channels the bot is in |
| `message.groups` | Messages in private channels the bot is in |
| `message.im` | Direct messages to the bot |
| `message.mpim` | Group DMs the bot is in |

4. Click **Save Changes**.

---

## Step 5: Install the App to Your Workspace

1. Navigate to **Settings → Install App**.
2. Click **Install to Workspace** and authorize the requested permissions.
3. Copy the **Bot User OAuth Token** — it starts with **`xoxb-`**. This is your `bot_token`.

---

## Step 6: Configure Genie

Add the following to your `.genie.toml` configuration file:

```toml
[messenger]
platform = "slack"

[messenger.slack]
app_token = "xapp-1-A0XXXXXXXXX-0000000000000-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
bot_token = "xoxb-0000000000000-0000000000000-XXXXXXXXXXXXXXXXXXXXXXXX"
```

> **Security:** Store tokens securely. You can also use environment variables by referencing them in your deployment configuration.

---

## Step 7: Invite the Bot to Channels

Before Genie can respond in a channel, the bot must be invited:

1. Open the desired Slack channel.
2. Type `/invite @Genie Bot` (or whatever you named your app).
3. Alternatively, mention `@Genie Bot` — Slack will prompt you to invite it.

For **direct messages**, simply open a DM with the bot — no invitation is needed.

---

## Verification

After starting Genie, you should see the log message:

```
connected to Slack via Socket Mode
```

Send a message to the bot in Slack to confirm everything is working.

---

## Troubleshooting

| Issue | Solution |
|---|---|
| `slack.app_token should start with xapp-` | Ensure you copied the **App-Level Token**, not the Bot Token. |
| `slack.bot_token should start with xoxb-` | Ensure you copied the **Bot User OAuth Token** from the Install App page. |
| Bot doesn't respond in a channel | Make sure the bot has been **invited** to the channel. |
| No events received | Verify **Socket Mode** is enabled and the correct **event subscriptions** are configured. |
| `socket mode connection error` | Check that your `app_token` has the `connections:write` scope. |

---

## Required Tokens Summary

| Token | Format | Where to Find |
|---|---|---|
| `app_token` | `xapp-…` | Settings → Socket Mode → App-Level Token |
| `bot_token` | `xoxb-…` | Settings → Install App → Bot User OAuth Token |
