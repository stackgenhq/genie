// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"sync"
	"time"
)

// pendingAuthTTL is how long a pending OAuth flow remains valid.
const pendingAuthTTL = 5 * time.Minute

// PendingAuth represents an in-flight OAuth authorization flow.
// The state parameter maps back to this struct so the callback
// can complete the authorization and save the token for the correct user.
type PendingAuth struct {
	// UserID is the user who initiated the flow (from MessageOrigin.Sender.ID).
	UserID string
	// ServiceName is the MCP server / service name.
	ServiceName string
	// SessionMarshal is the marshaled goth.Session string, used by goth-based stores.
	SessionMarshal string
	// CodeVerifier is the PKCE code verifier, used by the MCPOAuth (DCR) store.
	CodeVerifier string
	// ExpiresAt is when this pending auth expires.
	ExpiresAt time.Time
}

// PendingAuthStore manages pending OAuth authorization flows.
// It maps state parameters to PendingAuth entries with automatic
// TTL-based expiry.
type PendingAuthStore struct {
	mu      sync.RWMutex
	entries map[string]PendingAuth // state → PendingAuth
}

// NewPendingAuthStore creates a new pending auth store.
func NewPendingAuthStore() *PendingAuthStore {
	return &PendingAuthStore{
		entries: make(map[string]PendingAuth),
	}
}

// Store saves a pending auth entry keyed by state.
func (s *PendingAuthStore) Store(state string, entry PendingAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[state] = entry
}

// Load retrieves and removes a pending auth entry by state.
// Returns false if the entry does not exist or has expired.
func (s *PendingAuthStore) Load(state string) (PendingAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[state]
	if !ok {
		return PendingAuth{}, false
	}
	delete(s.entries, state)

	if time.Now().After(entry.ExpiresAt) {
		return PendingAuth{}, false
	}
	return entry, true
}

// Cleanup removes expired entries. Call periodically to prevent leaks.
func (s *PendingAuthStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for state, entry := range s.entries {
		if now.After(entry.ExpiresAt) {
			delete(s.entries, state)
		}
	}
}
