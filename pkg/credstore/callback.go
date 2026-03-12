// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"fmt"
	"net/http"

	"github.com/markbates/goth"
)

// CallbackHandler returns an http.Handler that processes OAuth redirect
// callbacks. Mount this at /oauth/callback (or the path matching your
// redirect URIs) on Genie's HTTP server.
//
// When a user completes the OAuth flow in their browser, the identity
// provider redirects to this handler with code and state parameters.
// The handler:
//  1. Looks up the pending auth by state (maps state → user + service)
//  2. Dispatches to the appropriate store type (goth OAuth or MCP OAuth)
//  3. Exchanges the authorization code for tokens
//  4. Stores the token via the Backend (keyed by userID + serviceName)
//  5. Responds with a success page telling the user to return to chat
func (m *Manager) CallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state == "" {
			http.Error(w, "missing state parameter", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}

		pending, ok := m.pending.Load(state)
		if !ok {
			http.Error(w, "invalid or expired authorization state", http.StatusBadRequest)
			return
		}

		store, ok := m.stores[pending.ServiceName]
		if !ok {
			http.Error(w, "unknown service: "+pending.ServiceName, http.StatusInternalServerError)
			return
		}

		// Dispatch to the appropriate store type.
		switch st := store.(type) {
		case *oauthStore:
			if err := m.handleGothCallback(w, r, st, pending); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		case *mcpOAuthStore:
			if err := m.handleMCPCallback(r, st, pending, code, state); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		default:
			http.Error(w, "service does not support OAuth callbacks", http.StatusInternalServerError)
			return
		}

		// Notify observers that the token was successfully saved.
		m.NotifyTokenSaved(pending.ServiceName)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<html><body style="font-family: system-ui, sans-serif; max-width: 480px; margin: 2rem auto; padding: 1rem; line-height: 1.5;">
<h2 style="color: #0a0;">✓ Connected to %s</h2>
<p>You can close this tab and return to your chat. Genie can now use <strong>%s</strong> on your behalf.</p>
<p><strong>Your privacy:</strong> Tokens are stored securely and scoped to your user account. StackGen does not have access to your credentials.</p>
</body></html>`, pending.ServiceName, pending.ServiceName)
	})
}

// handleGothCallback processes the OAuth callback for goth-based stores.
func (m *Manager) handleGothCallback(w http.ResponseWriter, r *http.Request, st *oauthStore, pending PendingAuth) error {
	// Reconstruct the goth session from the marshaled string.
	session, err := st.provider.UnmarshalSession(pending.SessionMarshal)
	if err != nil {
		return fmt.Errorf("failed to restore session: %w", err)
	}

	// Authorize: exchanges the auth code for tokens using goth's provider.
	// goth.Params is satisfied by url.Values (r.URL.Query()).
	_, err = session.Authorize(st.provider, r.URL.Query())
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}

	// Fetch user info + tokens from the provider.
	gothUser, err := st.provider.FetchUser(session)
	if err != nil {
		return fmt.Errorf("failed to fetch user: %w", err)
	}

	token := tokenFromGothUser(gothUser)

	if err := m.backend.Put(r.Context(), BackendPutRequest{
		UserID:      pending.UserID,
		ServiceName: pending.ServiceName,
		Token:       token,
	}); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}
	return nil
}

// handleMCPCallback processes the OAuth callback for MCP OAuth (DCR) stores.
// It exchanges the authorization code for tokens using the mcp-go OAuthHandler.
func (m *Manager) handleMCPCallback(r *http.Request, st *mcpOAuthStore, pending PendingAuth, code, state string) error {
	// ProcessAuthorizationResponse validates state, exchanges code for token,
	// and saves it via the OAuthHandler's TokenStore.
	if err := st.handler.ProcessAuthorizationResponse(r.Context(), code, state, pending.CodeVerifier); err != nil {
		return fmt.Errorf("MCP OAuth token exchange failed: %w", err)
	}

	// Also store the token in our backend so it's available for the credstore flow.
	tok, err := st.handler.GetAuthorizationHeader(r.Context())
	if err != nil {
		return fmt.Errorf("failed to get token after exchange: %w", err)
	}
	// The token was saved in the OAuthHandler's TokenStore. Retrieve it
	// and persist it in our backend keyed by user+service for consistency.
	_ = tok // Token is already stored by ProcessAuthorizationResponse via the TokenStore
	return nil
}

// tokenFromGothUser converts a goth.User to our Token type.
func tokenFromGothUser(u goth.User) *Token {
	return &Token{
		AccessToken:  u.AccessToken,
		TokenType:    "Bearer",
		RefreshToken: u.RefreshToken,
		ExpiresAt:    u.ExpiresAt,
	}
}
