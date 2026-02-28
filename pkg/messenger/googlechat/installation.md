# Google Chat — Installation & Configuration Guide

This guide walks you through connecting Genie to Google Chat using **HTTP push** for incoming events and the **Chat API** for outgoing messages. A public-facing HTTP endpoint is required to receive push events from Google Chat.

---

## Prerequisites

- A **Google Workspace** account (Google Chat is not available on personal Gmail accounts).
- Access to **Google Cloud Console** ([https://console.cloud.google.com](https://console.cloud.google.com)).
- A **public endpoint** (URL) where Google Chat can send push events — this can be achieved via:
  - A cloud VM or container with a public IP
  - A reverse proxy / tunnel (e.g., ngrok for development)
- Access to your Genie configuration file (`.genie.toml`).

---

## Step 1: Create a Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com).
2. Click the project dropdown → **New Project**.
3. Enter a project name (e.g., `genie-bot`) and click **Create**.
4. Select the newly created project.

---

## Step 2: Enable the Google Chat API

1. Navigate to **APIs & Services → Library**.
2. Search for **Google Chat API**.
3. Click **Google Chat API** → **Enable**.

---

## Step 3: OAuth credentials (same as Gmail/Calendar/Drive)

Google Chat uses the **logged-in user** OAuth token — the same one used for Gmail, Calendar, and Drive. No service account is required.

1. In **APIs & Services → Credentials**, create an **OAuth 2.0 Client ID** (Desktop app or Web application, with redirect URI as used by `genie grant`).
2. Build Genie with `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` set (or configure them in your deployment).
3. Run `genie grant` (or complete the Google sign-in during `genie setup`) so the token is stored. The default browser flow includes the `chat.messages` scope.

---

## Step 4: Configure the Chat App

1. Navigate to **APIs & Services → Google Chat API** → **Configuration** tab (or search for "Google Chat API" in the console and click **Manage**).
2. Fill in the app settings:

| Setting | Value |
|---|---|
| **App name** | `Genie` (or your preferred name) |
| **Avatar URL** | *(Optional)* URL to your bot's avatar image |
| **Description** | `AI-powered assistant` |
| **Functionality** | Check **Receive 1:1 messages** and **Join spaces and group conversations** |
| **Connection settings** | Select **HTTP endpoint URL** |
| **HTTP endpoint URL** | `https://your-domain.com/` (your public endpoint — Genie listens on `/`) |
| **Visibility** | Choose who can discover and use the bot in your organization |

3. Click **Save**.

---

## Step 5: Configure Genie

Add the following to your `.genie.toml` configuration file:

```toml
[messenger]
platform = "googlechat"

[messenger.googlechat]
# No fields; uses logged-in user token (SecretProvider).
```

No `credentials_file` is used. Genie uses the same logged-in user token as Gmail/Calendar/Drive (from `genie grant` or TokenFile/keyring). Ensure you have signed in with Google so the token includes the Chat scope.

---

## Step 6: Add the Bot to a Space

1. In Google Chat, open an existing space or create a new one.
2. Click the space name → **Manage members** → **Add people & bots**.
3. Search for your bot's name (e.g., `Genie`).
4. Select it and click **Add**.

For **1:1 conversations**, find the bot in the Chat search bar and start a direct message.

---

## Verification

After starting Genie, you should see the log messages:

```
starting Google Chat HTTP push listener addr=:8080
connected to Google Chat
```

Send a message to the bot in Google Chat to confirm everything is working.

---

## Troubleshooting

| Issue | Solution |
|---|---|
| `Google Chat requires WithSecretProvider` | The app passes the secret provider when creating the messenger; ensure you are running the official Genie binary that wires this. |
| `google chat token` / `google chat credentials` | Sign in with Google via `genie grant` (or setup). Ensure the OAuth client has the Chat API scope and the token is stored (keyring or TokenFile). |
| `failed to create Google Chat service` | Ensure the **Chat API** is enabled in your GCP project and your OAuth client is configured for the Chat API. |
| Bot doesn't receive messages | Verify the **HTTP endpoint URL** in the Chat API configuration matches your public URL. |
| `405 Method Not Allowed` | Google Chat sends **POST** requests. Ensure your endpoint accepts POST. |
| Bot not visible in Chat | Check the **Visibility** settings in the Chat API configuration. The bot may be restricted to specific users or OUs. |

---

## Required Configuration Summary

| Item | Where to Find |
|---|---|
| Logged-in user token | Run `genie grant` or complete Google sign-in during setup; token is stored in keyring or TokenFile. |
| Chat API HTTP endpoint | GCP Console → APIs & Services → Google Chat API → Configuration |

---

## Network Requirements

Genie must be reachable from the internet for Google Chat to deliver push events. Ensure your firewall or security group allows inbound HTTPS traffic to the shared HTTP server.

Google Chat push events originate from Google's infrastructure. Refer to [Google's IP ranges](https://support.google.com/a/answer/10026322) if you need to allowlist source IPs.
