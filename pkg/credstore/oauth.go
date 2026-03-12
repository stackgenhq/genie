// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"
	"fmt"
	"time"

	"github.com/markbates/goth"

	"golang.org/x/oauth2"
)

// oauthStore implements Store with per-user OAuth2 tokens. It uses goth's
// Provider abstraction for pre-configured multi-provider support (40+
// providers including GitHub, Google, Azure AD, AWS, GitLab, etc.).
//
// It resolves the user from ctx via MessageOrigin.Sender.ID and stores
// tokens in the configured Backend keyed by (userID, serviceName).
//
// When no token is present, GetToken returns an *AuthRequiredError
// containing the authorization URL. The caller sends this URL to the
// user (e.g. via Slack). The OAuth callback handler (see callback.go)
// completes the flow and stores the token.
type oauthStore struct {
	serviceName string
	backend     Backend
	provider    goth.Provider
	pending     *PendingAuthStore
}

// NewOAuthStoreRequest is the request for NewOAuthStore.
type NewOAuthStoreRequest struct {
	ServiceName string
	Backend     Backend
	// Provider is a goth.Provider instance (e.g. github.New(...), google.New(...)).
	// See https://github.com/markbates/goth/tree/master/providers for all
	// available providers.
	Provider goth.Provider
	Pending  *PendingAuthStore
}

// NewOAuthStore creates a Store backed by per-user OAuth2 tokens.
// The provider parameter should be a pre-configured goth.Provider
// (e.g. github.New(key, secret, callbackURL)).
func NewOAuthStore(req NewOAuthStoreRequest) Store {
	return &oauthStore{
		serviceName: req.ServiceName,
		backend:     req.Backend,
		provider:    req.Provider,
		pending:     req.Pending,
	}
}

// GetToken returns the token for the current user. If no token exists,
// it initiates the OAuth flow by returning an *AuthRequiredError with
// the authorization URL.
func (s *oauthStore) GetToken(ctx context.Context) (*Token, error) {
	userID := userIDFromContext(ctx)

	tok, err := s.backend.Get(ctx, BackendGetRequest{
		UserID:      userID,
		ServiceName: s.serviceName,
	})
	if err == nil && tok != nil && !tok.IsExpired() && tok.AccessToken != "" {
		return tok, nil
	}

	// If we have a refresh token, try refreshing via goth's provider.
	if err == nil && tok != nil && tok.RefreshToken != "" && s.provider.RefreshTokenAvailable() {
		refreshed, refreshErr := s.refreshToken(ctx, tok)
		if refreshErr == nil {
			return refreshed, nil
		}
		// Refresh failed — fall through to re-auth.
	}

	// Begin the OAuth flow via goth.
	state := oauth2.GenerateVerifier() // random state nonce
	session, err := s.provider.BeginAuth(state)
	if err != nil {
		return nil, fmt.Errorf("credstore: failed to begin auth for %s: %w", s.serviceName, err)
	}

	authURL, err := session.GetAuthURL()
	if err != nil {
		return nil, fmt.Errorf("credstore: failed to get auth URL for %s: %w", s.serviceName, err)
	}

	// Store pending auth state so the callback can complete the flow.
	s.pending.Store(state, PendingAuth{
		UserID:         userID,
		ServiceName:    s.serviceName,
		SessionMarshal: session.Marshal(),
		ExpiresAt:      time.Now().Add(pendingAuthTTL),
	})

	return nil, &AuthRequiredError{
		AuthURL:     authURL,
		ServiceName: s.serviceName,
	}
}

// SaveToken stores a token for the current user.
func (s *oauthStore) SaveToken(ctx context.Context, token *Token) error {
	userID := userIDFromContext(ctx)
	return s.backend.Put(ctx, BackendPutRequest{
		UserID:      userID,
		ServiceName: s.serviceName,
		Token:       token,
	})
}

// DeleteToken removes the token for the current user.
func (s *oauthStore) DeleteToken(ctx context.Context) error {
	userID := userIDFromContext(ctx)
	return s.backend.Delete(ctx, BackendDeleteRequest{
		UserID:      userID,
		ServiceName: s.serviceName,
	})
}

// ServiceName returns the service name.
func (s *oauthStore) ServiceName() string {
	return s.serviceName
}

// refreshToken uses goth's RefreshToken to get a new access token.
func (s *oauthStore) refreshToken(ctx context.Context, tok *Token) (*Token, error) {
	newOAuth2Token, err := s.provider.RefreshToken(tok.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("credstore: refresh failed for %s: %w", s.serviceName, err)
	}

	result := &Token{
		AccessToken:  newOAuth2Token.AccessToken,
		TokenType:    newOAuth2Token.TokenType,
		RefreshToken: newOAuth2Token.RefreshToken,
		ExpiresAt:    newOAuth2Token.Expiry,
	}
	// If refresh token wasn't rotated, keep the old one.
	if result.RefreshToken == "" {
		result.RefreshToken = tok.RefreshToken
	}

	// Persist the refreshed token.
	userID := userIDFromContext(ctx)
	_ = s.backend.Put(ctx, BackendPutRequest{
		UserID:      userID,
		ServiceName: s.serviceName,
		Token:       result,
	})
	return result, nil
}
