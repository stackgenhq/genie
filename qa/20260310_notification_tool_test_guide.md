# Notification Tool QA Testing Guide

This document outlines the testing procedures for the `notification` tool, which supports sending alerts via Slack, Webhooks, Discord, and Twilio.

## Prerequisites

1.  **Genie Configuration**: Ensure you have a valid `genie.toml` or `genie.yaml` file to configure the notification providers. You can use the Config Builder UI ([docs/config-builder.html](../docs/config-builder.html)) to generate it.
2.  **Notification Persona (Required)**: Out of the box, the default agent resume might not include notification capabilities, causing the AI front-desk to reject your request as "out of scope." To fix this, create a file named `persona.md` containing:
    ```markdown
    You are a DevOps assistant. You have access to a notification tool and can send messages, alerts, and notifications to platforms like Slack, Discord, Webhooks, and Twilio. Always use the notification tool when asked to notify users or send a message. 
    
    CRITICAL RULES:
    - NEVER ask the user clarifying questions about who should receive the notification, how to send it, what channel to use, or what specific platform to utilize. The notification tool will automatically route the message to the configured default destinations.
    - If the user provides a vague message, simply construct a reasonable and comprehensive message out of it yourself.
    - If the user omits a justification or agent_name, invent reasonable defaults (e.g., agent_name: "DevOps-Assistant", justification: "System Alert").
    ```
    And configure it in your `genie.toml`:
    ```toml
    [persona]
    file = "persona.md"
    disable_resume = true
    ```
3.  **Provider Credentials**: You will need valid credentials (webhook URLs, API keys, etc.) for each provider you intend to test.

## Section 1: Slack Integration

### Setup
1. Create an Incoming Webhook in a Slack workspace or obtain an existing one.
2. Configure `genie.toml`:
```toml
[[notification.slack]]
webhook_url = "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
```

### Test Case 1.1: Direct Tool Invocation (Slack)
1. Start Genie with the configured `genie.toml` and a test persona.
2. Provide a prompt to the agent: `@genie Use your notification tool to send a message to Slack that says "Testing Genie Slack integration" with the justification "Verification testing".`
3. **Expected Result**: 
    - The agent should successfully execute the `notify` tool.
    - A message should appear in the configured Slack channel in the format:
      ```
      Agent [Agent Name] requires assistance.
      Justification: Verification testing
      Message: Testing Genie Slack integration
      ```

## Section 2: Webhooks Integration

### Setup
1. Set up a webhook endpoint (e.g., using a service like `webhook.site` or a local standard HTTP server that can receive POST requests).
2. Configure `genie.toml`:
```toml
[[notification.webhooks]]
url = "https://webhook.site/YOUR-UUID"

[notification.webhooks.headers]
Authorization = "Bearer test-token"
Custom-Header = "QA-Test"
```

### Test Case 2.1: Direct Tool Invocation (Webhook)
1. Start Genie with the configured `genie.toml`.
2. Provide a prompt: `@genie Use your notification tool to send an alert to the configured webhook saying "Webhook test message" with the justification "Webhook verification".`
3. **Expected Result**: 
    - The agent executes the tool without errors.
    - Check the webhook endpoint logs. It should receive a POST request with the configured headers.
    - The JSON payload (or text payload) should contain the message, agent name, and justification.

## Section 3: Discord Integration

### Setup
1. Create a Webhook in a Discord channel (Server Settings -> Integrations -> Webhooks).
2. Configure `genie.toml`:
```toml
[[notification.discord]]
webhook_url = "https://discord.com/api/webhooks/YOUR/WEBHOOK/URL"
```

### Test Case 3.1: Direct Tool Invocation (Discord)
1. Start Genie with the configured `genie.toml`.
2. Provide a prompt: `@genie Use the notification tool to alert Discord with the message "Discord integration test" and justification "Testing Discord notifications".`
3. **Expected Result**: 
    - The agent executes the tool correctly.
    - A message is posted in the configured Discord channel formatted similarly to the Slack message.

## Section 4: Twilio Integration (SMS)

### Setup
1. Obtain Twilio Account SID, Auth Token, and a valid "From" Twilio phone number. You also need a verified "To" phone number if using a trial account.
2. Configure `genie.toml` (using environment variables is recommended for secrets):
```toml
[[notification.twilio]]
account_sid = "${TWILIO_ACCOUNT_SID}"
auth_token = "${TWILIO_AUTH_TOKEN}"
from = "+1234567890" # Your Twilio number
to = "+0987654321"   # Destination number
```
Ensure the environment variables are exported before starting.

### Test Case 4.1: Direct Tool Invocation (Twilio)
1. Start Genie with the configured `genie.toml` and necessary environment variables.
2. Provide a prompt: `@genie Send an SMS notification using Twilio that says "Twilio integration test" with the justification "Testing SMS".`
3. **Expected Result**: 
    - The agent executes the tool without errors.
    - An SMS message should be delivered to the configured `to` number containing the message and justification.

## Section 5: Multiple Providers

### Setup
Configure at least two providers in your `genie.toml` (e.g., Slack and a Webhook).

### Test Case 5.1: Broadcast Notification
1. Start Genie with the multi-provider configuration.
2. Provide a prompt: `@genie Trigger a notification with the message "Broadcast test" and justification "Testing multiple providers".`
3. **Expected Result**: 
    - The agent executes the `notify` tool.
    - *All* configured providers should receive the notification successfully. For example, a Slack message appears AND the webhook receives a payload simultaneously.

## Section 6: Error Handling

### Test Case 6.1: Invalid Provider Credentials
1. Configure a provider with invalid credentials (e.g., a dummy Slack webhook URL).
2. Run a notification test.
3. **Expected Result**: The notification for the misconfigured provider should fail and generate an error or warning log indicating the failure for that specific provider, but the overall tool execution may still be reported as successful as long as at least one other configured provider succeeds.

### Test Case 6.2: Missing Required Fields
If trying specifically to invoke the tool directly, instruct the agent to omit the justification.
1. Provide a prompt: `@genie Call the notify tool with the message "Testing missing fields" but DO NOT provide a justification.`
2. **Expected Result**: The tool execution should fail with a schema validation error (or similar), as `justification` is a required field in the tool declaration.
