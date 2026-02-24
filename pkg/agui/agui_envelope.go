package agui

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CloudEvent wraps an AG-UI event payload in a CloudEvents v1.0 envelope.
// See https://cloudevents.io for the specification.
//
// This is used when forwarding agent events to external systems (event buses,
// audit pipelines, observability stacks) that expect CloudEvents.
// The TUI and TCP listener continue to use raw AG-UI events internally.
type CloudEvent struct {
	// SpecVersion is the CloudEvents specification version (always "1.0").
	SpecVersion string `json:"specversion"`
	// ID is a unique identifier for this event (UUID v4).
	ID string `json:"id"`
	// Source identifies the context in which the event happened, e.g.
	// "genie/reactree/stage-2" or "genie/orchestrator".
	Source string `json:"source"`
	// Type is the CloudEvents type, e.g. "ai.genie.agui.RUN_STARTED".
	Type string `json:"type"`
	// Time is the timestamp when the event was produced.
	Time time.Time `json:"time"`
	// Data is the AG-UI event payload.
	Data interface{} `json:"data"`
}

// WrapInCloudEvent wraps an AGUIEvent in a CloudEvents v1.0 envelope.
// The source parameter identifies where the event originated (e.g.
// "genie/reactree/stage-2"). If the event does not implement AGUIEvent,
// the type is set to "ai.genie.agui.CUSTOM".
func WrapInCloudEvent(evt interface{}, source string) CloudEvent {
	aguiType := EventCustom
	if ae, ok := evt.(AGUIEvent); ok {
		aguiType = ae.AGUIType()
	}

	return CloudEvent{
		SpecVersion: "1.0",
		ID:          uuid.New().String(),
		Source:      source,
		Type:        fmt.Sprintf("ai.genie.agui.%s", aguiType),
		Time:        time.Now(),
		Data:        evt,
	}
}
