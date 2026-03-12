// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"
	"time"
)

//go:generate go tool counterfeiter -generate

// Store provides per-user, per-service credential management.
// It resolves the current user from ctx (via MessageOrigin.Sender.ID)
// and the service from the configured service name.
//
// Store is intentionally compatible with mcp-go's transport.TokenStore
// interface (same GetToken/SaveToken signatures) so it can be used
// directly as a TokenStore for MCP OAuth clients.
//
//counterfeiter:generate . Store
type Store interface {
	// GetToken returns the token for the current user + service.
	// Returns ErrNoToken if no token is available and no OAuth is configured.
	// Returns *AuthRequiredError (with AuthURL) if OAuth is configured but
	// the user has not yet authenticated.
	GetToken(ctx context.Context) (*Token, error)

	// SaveToken stores a token for the current user + service.
	SaveToken(ctx context.Context, token *Token) error

	// DeleteToken removes the token for the current user + service.
	// Used for sign-out or token revocation.
	DeleteToken(ctx context.Context) error

	// ServiceName returns the name of the service this store manages
	// (e.g. "github", "jira"). Used as a namespace for token storage.
	ServiceName() string
}

// Token represents an OAuth/Bearer token with optional refresh and expiry.
type Token struct {
	// AccessToken is the token value (Bearer token, PAT, API key).
	AccessToken string `json:"access_token"`
	// TokenType is the type of token (usually "Bearer").
	TokenType string `json:"token_type"`
	// RefreshToken is used to obtain a new access token when expired.
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresIn is the number of seconds until the token expires.
	ExpiresIn int64 `json:"expires_in,omitempty"`
	// ExpiresAt is the absolute time when the token expires.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	// Scope is the granted scope of the token.
	Scope string `json:"scope,omitempty"`
}

// IsExpired reports whether the token has expired.
func (t *Token) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}
