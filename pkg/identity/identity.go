// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package identity provides a unified user identity type (Sender) and
// context helpers shared by the auth middleware and the messenger subsystem.
// It exists as a standalone package to avoid import cycles between
// pkg/security/auth (which produces identities) and pkg/messenger (which
// consumes them in MessageOrigin).
package identity

import (
	"context"
)

// Sender represents the authenticated user or entity interacting with Genie.
// It unifies platform-level identity (ID, Username, DisplayName) with security
// metadata (Role, AuthenticatedVia) so that a single type flows through both
// HTTP-authenticated and messenger-originated requests.
type Sender struct {
	// ID is a unique identifier for the user (e.g. Email, Username, API Key abbreviation, or "demo-user").
	ID string
	// Username is the unique handle (e.g., Slack member ID, Discord username).
	Username string
	// DisplayName is a human-readable display name, if available.
	DisplayName string
	// Role defines the access tier of the user (e.g. "admin", "user", "agent", "demo").
	Role string
	// AuthenticatedVia describes which strategy verified this sender
	// (e.g. "oidc", "jwt", "apikey", "password", "none").
	AuthenticatedVia string
}

// senderContextKey is an unexported type for context keys defined in this package.
// This prevents collisions with keys defined in other packages.
type senderContextKey struct{}

// WithSender returns a new context with the assigned Sender.
func WithSender(ctx context.Context, s Sender) context.Context {
	return context.WithValue(ctx, senderContextKey{}, s)
}

// GetSender retrieves the Sender from the context.
// If no sender is found (meaning authentication was completely disabled or
// the call originated from a messenger path without auth), it returns a safe
// "demo" user to ensure the rest of the chain doesn't panic on a zero value.
func GetSender(ctx context.Context) Sender {
	val := ctx.Value(senderContextKey{})
	if s, ok := val.(Sender); ok {
		return s
	}
	return DemoSender()
}

// DemoSender returns a Sender that represents the unauthenticated demo user.
func DemoSender() Sender {
	return Sender{
		ID:               "demo-user",
		DisplayName:      "Demo User",
		Role:             "demo",
		AuthenticatedVia: "none",
	}
}

// IsDemo returns true if this sender is the default unauthenticated Demo User.
func (s Sender) IsDemo() bool {
	return s.Role == "demo"
}
