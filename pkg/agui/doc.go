// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package agui provides types and helpers for the Agent GUI (AG-UI) event protocol
// used by Genie to stream agent state to UIs (TUI, web, external systems).
//
// It solves the problem of standardizing how agent events (run started, tool
// calls, thinking, errors) are represented and forwarded. Events can be wrapped
// in CloudEvents v1.0 envelopes for integration with event buses, audit
// pipelines, and observability stacks that expect CloudEvents.
//
// Key types: AgentThinkingMsg, WrapInCloudEvent. The TUI and TCP listener
// consume raw AG-UI events; external systems receive CloudEvents-wrapped payloads.
package agui
