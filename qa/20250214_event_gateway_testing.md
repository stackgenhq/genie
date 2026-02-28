# Event Gateway Feature Testing Guide

This document provides instructions for testing the **Event Gateway** and **Heartbeat** features in Genie.

## Features

1.  **Event Gateway**: An HTTP endpoint (`/api/v1/events`) that accepts webhook payloads (e.g., from GitHub, alerting systems) and triggers an agent execution.
2.  **Heartbeat**: A scheduled background task that runs every 10 minutes to verify system health or perform maintenance tasks.

## Prerequisites

- Genie must be running:
  ```bash
  make run
  # or
  ./genie server
  ```

## 1. Unit Testing

Unit tests cover the core logic, event mapping, and endpoint handling. These are the primary verification method.

To run the unit tests:
```bash
go test -v ./pkg/agui/...
```
Verify that all tests pass, including:
- `should accept valid events at /api/v1/events`
- `should process a valid event` (Webhook)
- `should process a heartbeat event`

## 2. Manual Verification (Blackbox)

You can manually trigger events using `curl` to verify the end-to-end flow. Since background agents output to logs, you will need to monitor the server logs.

### A. Testing the Webhook Endpoint

1.  **Send a Webhook Payload**
    Send a POST request to the local server with a JSON payload. The `type` must be provided. The `payload` can be any valid JSON object.

    ```bash
    curl -i -X POST http://localhost:8080/api/v1/events \
      -H "Content-Type: application/json" \
      -d '{
        "type": "webhook",
        "source": "manual_test",
        "payload": {
          "alert": "high_cpu_usage",
          "server": "prod-01"
        }
      }'
    ```

2.  **Expected Response**
    You should receive a `202 Accepted` response with a JSON body containing a `run_id`.

    ```json
    {
      "status": "accepted",
      "run_id": "uuid-..."
    }
    ```

3.  **Verify Logs**
    Check the terminal running the Genie server. You should see logs indicating the agent has started processing the event:

    ```text
    INFO  Handling event type=webhook source=manual_test
    INFO  Background agent started runId=...
    INFO  [AG-UI] System Event [webhook from manual_test]: {"alert":"high_cpu_usage",...}
    ```

### B. Testing Heartbeats

The heartbeat runs automatically every 10 minutes.

1.  **Monitor Logs**
    Keep the server running and watch for periodic logs:

    ```text
    INFO  Triggering scheduled heartbeat event
    INFO  Handling event type=heartbeat source=system_ticker
    ```

    *Note: To test this more quickly during development, you may modify `pkg/agui/server.go` to reduce the ticker interval (e.g., to `10 * time.Second`).*

## Troubleshooting

- **400 Bad Request**: Ensure your JSON payload is valid and includes a `type` field.
- **503 Service Unavailable** (or 429): The background worker pool might be full. In the default configuration, only 2 concurrent background agents are allowed. Wait for previous tasks to finish.
