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
//  2. Reconstructs the goth session and authorizes the code exchange
//  3. Fetches user info and tokens via goth.Provider.FetchUser
//  4. Stores the token via the Backend (keyed by userID + serviceName)
//  5. Responds with a success page telling the user to return to chat
func (m *Manager) CallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state == "" {
			http.Error(w, "missing state parameter", http.StatusBadRequest)
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

		oauthSt, ok := store.(*oauthStore)
		if !ok {
			http.Error(w, "service does not support OAuth", http.StatusInternalServerError)
			return
		}

		// Reconstruct the goth session from the marshaled string.
		session, err := oauthSt.provider.UnmarshalSession(pending.SessionMarshal)
		if err != nil {
			http.Error(w, "failed to restore session: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Authorize: exchanges the auth code for tokens using goth's provider.
		// goth.Params is satisfied by url.Values (r.URL.Query()).
		_, err = session.Authorize(oauthSt.provider, r.URL.Query())
		if err != nil {
			http.Error(w, "authorization failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch user info + tokens from the provider.
		gothUser, err := oauthSt.provider.FetchUser(session)
		if err != nil {
			http.Error(w, "failed to fetch user: "+err.Error(), http.StatusInternalServerError)
			return
		}

		token := tokenFromGothUser(gothUser)

		if err := m.backend.Put(r.Context(), BackendPutRequest{
			UserID:      pending.UserID,
			ServiceName: pending.ServiceName,
			Token:       token,
		}); err != nil {
			http.Error(w, "failed to store token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<html><body style="font-family: system-ui, sans-serif; max-width: 480px; margin: 2rem auto; padding: 1rem; line-height: 1.5;">
<h2 style="color: #0a0;">✓ Connected to %s</h2>
<p>You can close this tab and return to your chat. Genie can now use <strong>%s</strong> on your behalf.</p>
<p><strong>Your privacy:</strong> Tokens are stored securely and scoped to your user account. StackGen does not have access to your credentials.</p>
</body></html>`, pending.ServiceName, pending.ServiceName)
	})
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
