// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"sync"
)

// Manager creates and manages credential stores for multiple services.
// It provides a unified entry point for creating static or OAuth-backed
// stores and handles the OAuth callback endpoint.
type Manager struct {
	mu      sync.RWMutex
	stores  map[string]Store // serviceName → Store
	backend Backend
	pending *PendingAuthStore
}

// NewManagerRequest is the request for NewManager.
type NewManagerRequest struct {
	Backend Backend
}

// NewManager creates a new credential store manager.
func NewManager(req NewManagerRequest) *Manager {
	backend := req.Backend
	if backend == nil {
		backend = NewMemoryBackend()
	}
	return &Manager{
		stores:  make(map[string]Store),
		backend: backend,
		pending: NewPendingAuthStore(),
	}
}

// RegisterStaticRequest is the request for Manager.RegisterStatic.
type RegisterStaticRequest struct {
	ServiceName string
	SecretName  string
	Provider    interface {
		GetSecret(interface{}, interface{}) (string, error)
	}
}

// RegisterStatic creates and registers a static token store for the given service.
func (m *Manager) RegisterStatic(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stores[store.ServiceName()] = store
}

// RegisterOAuth creates and registers an OAuth2 token store for the given service.
// The goth.Provider carries all OAuth config (client ID, secret, scopes, endpoints).
func (m *Manager) RegisterOAuth(req NewOAuthStoreRequest) Store {
	req.Backend = m.backend
	req.Pending = m.pending
	store := NewOAuthStore(req)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.stores[store.ServiceName()] = store
	return store
}

// RegisterMCPOAuth creates and registers an MCP OAuth store that supports
// Dynamic Client Registration (RFC 7591). No client_id or client_secret
// is needed — they are obtained from the MCP server's registration endpoint.
func (m *Manager) RegisterMCPOAuth(req NewMCPOAuthStoreRequest) Store {
	req.Backend = m.backend
	req.Pending = m.pending
	store := NewMCPOAuthStore(req)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.stores[store.ServiceName()] = store
	return store
}

// StoreFor returns the Store for the given service name.
// Returns nil if no store is registered for the service.
func (m *Manager) StoreFor(serviceName string) Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stores[serviceName]
}

// Backend returns the backend used by this manager.
func (m *Manager) Backend() Backend {
	return m.backend
}

// Pending returns the pending auth store for this manager.
func (m *Manager) Pending() *PendingAuthStore {
	return m.pending
}
