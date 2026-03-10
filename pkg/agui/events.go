// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui

import "encoding/json"

// EventType defines the type of event (e.g., webhook, heartbeat).
type EventType string

const (
	EventTypeWebhook   EventType = "webhook"
	EventTypeHeartbeat EventType = "heartbeat"
)

// EventRequest represents the payload for an event.
// Used by cron dispatcher, background worker, and event gateway.
type EventRequest struct {
	Type    EventType       `json:"type"`
	Source  string          `json:"source"`            // e.g., "github", "cron"
	Payload json.RawMessage `json:"payload,omitempty"` // Flexible payload
}
