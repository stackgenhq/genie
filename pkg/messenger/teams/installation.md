# Microsoft Teams — Installation & Configuration Guide

This guide walks you through connecting Genie to Microsoft Teams using the **Bot Framework** protocol. A public-facing HTTP endpoint is required to receive incoming activities from Teams.

---

## Prerequisites

- A Microsoft 365 tenant with **Teams** enabled.
- An **Azure** account (free tier works for bot registration).
- A **public endpoint** (URL) where Teams can send webhook events — this can be achieved via:
  - A cloud VM or container with a public IP
  - A reverse proxy / tunnel (e.g., ngrok for development)
- Access to your Genie configuration file (`.genie.toml`).

---

## Step 1: Register a Bot in Azure

1. Go to the [Azure Portal](https://portal.azure.com).
2. Search for **Azure Bot** and click **Create**.
3. Fill in the registration form:
   - **Bot handle**: A unique identifier (e.g., `genie-bot`).
   - **Subscription**: Select your Azure subscription.
   - **Resource group**: Create a new one or select existing.
   - **Pricing tier**: **F0 (Free)** is sufficient for most use cases.
   - **Microsoft App ID**: Select **Create new Microsoft App ID**.
4. Click **Review + Create** → **Create**.

---

## Step 2: Get Your App ID and Password

1. After the bot resource is created, navigate to it in the Azure Portal.
2. Go to **Settings → Configuration**.
3. Copy the **Microsoft App ID** — this is your `app_id`.
4. Click **Manage Password** next to the App ID:
   - Click **New client secret**.
   - Enter a description (e.g., `genie-bot-secret`) and set an expiry.
   - Click **Add**.
   - Copy the **Value** immediately — this is your `app_password`. It won't be shown again.

---

## Step 3: Configure the Messaging Endpoint

1. In your Azure Bot resource, go to **Settings → Configuration**.
2. Set the **Messaging endpoint** to your public URL:
   ```
   https://your-domain.com/api/messages
   ```
   > The path **must** be `/api/messages` — this is where Genie listens for Bot Framework activities.
3. Click **Apply**.

### For Development (using ngrok)

If you don't have a public endpoint yet, you can use ngrok for testing:

```bash
ngrok http 3978
```

Then set the messaging endpoint to:
```
https://xxxx-xx-xx-xx-xx.ngrok-free.app/api/messages
```

---

## Step 4: Enable the Teams Channel

1. In your Azure Bot resource, go to **Channels**.
2. Click **Microsoft Teams** under "Available channels".
3. Accept the Terms of Service.
4. Click **Apply**.

---

## Step 5: Configure Genie

Add the following to your `.genie.toml` configuration file:

```toml
[messenger]
platform = "teams"

[messenger.teams]
app_id       = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
app_password = "your-client-secret-value"
listen_addr  = ":3978"
```

| Parameter | Description | Default |
|---|---|---|
| `app_id` | Microsoft Bot Framework App ID | *(required)* |
| `app_password` | Microsoft Bot Framework App Password (client secret) | *(required)* |
| `listen_addr` | Local address and port to listen on | `:3978` |

> **Security:** Never commit credentials to version control. Use environment variables or a secrets manager in production.

---

## Step 6: Install the Bot in Teams

### For your organization:

1. In the Azure Portal, go to your Bot resource → **Channels** → **Microsoft Teams** → **Open in Teams**.
2. Click **Add** to install the bot for yourself.

### For distribution across your organization:

1. Create a **Teams App Package** (a `.zip` file containing a `manifest.json`, icons, etc.).
2. Upload the app via **Teams Admin Center** → **Manage apps** → **Upload new app**.
3. Users in your tenant can then find and install the bot from the Teams app store.

---

## Verification

After starting Genie, you should see the log message:

```
connected to Teams via Bot Framework
starting Teams webhook listener addr=:3978
```

Send a direct message to the bot in Teams to confirm everything is working.

---

## Troubleshooting

| Issue | Solution |
|---|---|
| `teams.app_id is required` | Ensure you've set the App ID from the Azure Bot registration. |
| `teams.app_password is required` | Ensure you've set the client secret value (not the secret ID). |
| `failed to create Teams bot adapter` | Verify both `app_id` and `app_password` are correct. |
| Bot doesn't receive messages | Check that the **messaging endpoint** in Azure matches your public URL + `/api/messages`. |
| `401 Unauthorized` errors | The `app_password` may have expired. Generate a new client secret in Azure. |
| `failed to parse Teams activity` | Ensure incoming requests are reaching the `/api/messages` endpoint correctly. |

---

## Required Credentials Summary

| Credential | Where to Find |
|---|---|
| `app_id` | Azure Portal → Bot resource → Configuration → Microsoft App ID |
| `app_password` | Azure Portal → Bot resource → Configuration → Manage Password → Client secret value |

---

## Network Requirements

Genie must be reachable from the internet on the configured `listen_addr` for Teams to deliver messages. Ensure your firewall or security group allows inbound HTTPS traffic to the configured port (default: `3978`).
