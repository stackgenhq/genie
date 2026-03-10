// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

// Package interrupt defines the [InterruptError] sentinel used by tools and
// middleware to signal that a tool call needs external input (human approval,
// clarification answer, etc.) before it can complete.
//
// It lives in its own package — separate from toolwrap and clarify — to
// avoid import cycles. Both packages depend on this one, but neither depends
// on the other.
package interrupt

import (
	"errors"
	"fmt"
)

// Kind identifies what type of human input is required.
type Kind string

const (
	// Clarify indicates the tool needs the user to answer a clarifying
	// question before execution can continue.
	Clarify Kind = "clarify"

	// Approval indicates the tool call requires human approval
	// (approve / reject) before execution can proceed.
	Approval Kind = "approval"
)

// Error signals that a tool call needs external input (human approval,
// clarification, etc.) before it can complete.
//
// In the current in-process executor (blocking mode), the middleware
// catches this internally and blocks on the store's wait channel — callers
// never see it. In a future Temporal executor (non-blocking mode), the
// workflow propagates the interrupt upward so it can be modelled as a
// Temporal signal wait.
//
// The API mirrors [graph.InterruptError] from trpc-agent-go so that the
// two can be bridged without conversion when Temporal is adopted.
type Error struct {
	// Kind distinguishes clarification pauses from approval pauses.
	Kind Kind

	// RequestID is the unique identifier of the pending request
	// (clarification ID or approval ID).
	RequestID string

	// Payload carries kind-specific metadata:
	//   - Clarify:  clarify.ClarificationEvent
	//   - Approval: hitl.ApprovalRequest
	Payload any
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("interrupt(%s): waiting for human input on request %s", e.Kind, e.RequestID)
}

// Is reports whether err is or wraps an interrupt [Error].
func Is(err error) bool {
	var ie *Error
	return errors.As(err, &ie)
}

// Get extracts an interrupt [Error] from err if present.
// Returns (nil, false) if err does not contain an interrupt Error.
func Get(err error) (*Error, bool) {
	var ie *Error
	if errors.As(err, &ie) {
		return ie, true
	}
	return nil, false
}
