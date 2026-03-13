# WhatsApp — Installation & Configuration Guide

This guide walks you through connecting Genie to WhatsApp using the **WhatsApp Web multi-device protocol**. No public endpoint, API keys, or Meta Business account is required — Genie connects as a linked device, just like WhatsApp Web or Desktop.

---

## Prerequisites

- A **smartphone** with WhatsApp installed and an active account.
- A **terminal** to view the QR code during initial pairing.
- Access to your Genie configuration file (`.genie.toml`).

> **Note:** This adapter uses the WhatsApp Web protocol (not the WhatsApp Business API). It works with regular WhatsApp accounts.

---

## Step 1: Configure Genie

Add the following to your `.genie.toml` configuration file:

```toml
[messenger]
platform = "whatsapp"
```

| Parameter | Description | Default |
|---|---|---|

---

## Step 2: Initial QR Code Pairing

1. **Start Genie** — on the first run, you will see a QR code printed in your terminal.
2. Open **WhatsApp** on your phone:
   - Go to **Settings** → **Linked Devices**.
   - Tap **Link a Device**.
   - Scan the QR code displayed in the terminal.
3. Wait for the pairing to complete. You should see:

   ```
   WhatsApp QR pairing successful
   connected to WhatsApp via Web protocol
   ```

> **Timeout:** If the QR code expires before you scan it, restart Genie to generate a new one.

The QR code is also saved as a PNG image in your `store_path` directory for convenience.

---

## Step 3: Verify the Connection

After pairing, send a WhatsApp message to the phone number linked to Genie. The bot should respond.

On subsequent startups, Genie will automatically reconnect using the stored session:

```
reconnecting to WhatsApp with stored session
connected to WhatsApp via Web protocol
```

---

## Restricting Access (Optional)

By default, Genie responds to messages from **anyone**. To limit access to specific phone numbers, use the `allowed_senders` configuration:

```toml
[messenger]
platform = "whatsapp"
allowed_senders = ["15551234567", "44207XXXXXXX"]
```

- Phone numbers should be in **E.164 format without the `+`** prefix.
- Wildcard patterns are supported: `"1555*"` matches any number starting with `1555`.

---

## Supported Message Types

The WhatsApp adapter supports the following incoming message types:

| Type | Supported |
|---|---|
| Text messages | ✅ |
| Images | ✅ (downloaded locally) |
| Videos | ✅ (downloaded locally) |
| Audio / Voice notes | ✅ (downloaded locally) |
| Documents / Files | ✅ (downloaded locally) |
| Emoji reactions (👍, 👎, etc.) | ✅ |
| Quoted replies (swipe-to-reply) | ✅ |
| Group messages | ✅ |

Media files are automatically downloaded and saved to `<store_path>/media/`.

---

## Session Management

### Re-pairing

If you need to re-pair (e.g., after uninstalling WhatsApp or switching phones):

1. Delete the session database:

   ```bash
   rm -rf ~/.genie/whatsapp/whatsmeow.db
   ```

2. Restart Genie — a new QR code will be generated.

### Linked Device Limits

WhatsApp allows up to **4 linked devices** per account. If you've reached the limit:

1. Open **WhatsApp** → **Settings** → **Linked Devices**.
2. Remove an unused device.
3. Restart Genie to initiate pairing again.

---

## Verification

After starting Genie, you should see one of:

**First run (QR pairing):**

```
no stored WhatsApp session found, starting QR code pairing
scan the QR code below with your WhatsApp app
```

**Subsequent runs (stored session):**

```
reconnecting to WhatsApp with stored session
connected to WhatsApp via Web protocol
```

---

## Troubleshooting

| Issue | Solution |
|---|---|
| QR code not visible in terminal | Ensure your terminal supports Unicode characters. Try a different terminal emulator, or check the PNG file saved to `store_path`. |
| `QR code scan timed out` | The QR code has a limited validity. Restart Genie and scan more quickly. |
| `failed to create store dir` | Check file permissions on the `store_path` directory. |
| `failed to open store` | The SQLite database may be corrupted. Delete `whatsmeow.db` and re-pair. |
| Bot stops responding after a while | WhatsApp may disconnect idle linked devices. Ensure Genie stays running. Reconnection is automatic. |
| Messages not delivered in groups | Ensure the phone number running Genie is a **member** of the group. |

---

## Required Configuration Summary

| Item | Description |
|---|---|
| QR code scan | One-time pairing via WhatsApp → Settings → Linked Devices |

No API keys, tokens, or external service credentials are needed.
