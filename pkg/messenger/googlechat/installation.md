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

## Step 3: Create a Service Account

1. Navigate to **IAM & Admin → Service Accounts**.
2. Click **Create Service Account**.
3. Fill in the details:
   - **Name**: `genie-chat-bot`
   - **Description**: `Service account for Genie Chat bot`
4. Click **Create and Continue**.
5. *(Optional)* Grant roles if needed, then click **Done**.

### Generate a JSON Key

1. Click on the newly created service account.
2. Go to the **Keys** tab.
3. Click **Add Key → Create new key**.
4. Select **JSON** and click **Create**.
5. The key file will be downloaded — save it securely. This is your `credentials_file`.

> **Important:** This key file grants API access. Store it securely and never commit it to version control.

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
credentials_file = "/path/to/service-account-key.json"
listen_addr      = ":8080"
```

| Parameter | Description | Default |
|---|---|---|
| `credentials_file` | Path to the Google service account JSON key file | *(optional — can use Application Default Credentials)* |
| `listen_addr` | Local address and port for the HTTP push listener | *(required)* |

### Using Application Default Credentials (ADC)

If running on Google Cloud (GCE, Cloud Run, GKE, etc.), you can omit `credentials_file` and use ADC instead:

```bash
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/service-account-key.json"
```

Or rely on the attached service account when running on GCP infrastructure.

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
| `failed to read credentials file` | Verify the `credentials_file` path is correct and the file exists. |
| `failed to create Google Chat service` | Check that the **Chat API** is enabled in your GCP project and the service account has proper permissions. |
| Bot doesn't receive messages | Verify the **HTTP endpoint URL** in the Chat API configuration matches your public URL. |
| `405 Method Not Allowed` | Google Chat sends **POST** requests. Ensure your endpoint accepts POST. |
| Bot can't send messages | Ensure the service account is associated with the Chat app in the API configuration. |
| Bot not visible in Chat | Check the **Visibility** settings in the Chat API configuration. The bot may be restricted to specific users or OUs. |

---

## Required Configuration Summary

| Item | Where to Find |
|---|---|
| `credentials_file` | GCP Console → IAM & Admin → Service Accounts → Keys → JSON key download |
| Chat API HTTP endpoint | GCP Console → APIs & Services → Google Chat API → Configuration |

---

## Network Requirements

Genie must be reachable from the internet on the configured `listen_addr` for Google Chat to deliver push events. Ensure your firewall or security group allows inbound HTTPS traffic to the configured port (default: `8080`).

Google Chat push events originate from Google's infrastructure. Refer to [Google's IP ranges](https://support.google.com/a/answer/10026322) if you need to allowlist source IPs.
