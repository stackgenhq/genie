// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"
	"sync"
)

// MemoryBackend is an in-memory Backend suitable for tests and
// single-process deployments. Tokens are lost on process restart.
type MemoryBackend struct {
	mu     sync.RWMutex
	tokens map[string]*Token // key = "userID:serviceName"
}

// NewMemoryBackend creates a new in-memory token backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		tokens: make(map[string]*Token),
	}
}

func (b *MemoryBackend) storageKey(userID, serviceName string) string {
	return userID + ":" + serviceName
}

// Get returns the token for the given user and service.
func (b *MemoryBackend) Get(ctx context.Context, req BackendGetRequest) (*Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	tok, ok := b.tokens[b.storageKey(req.UserID, req.ServiceName)]
	if !ok {
		return nil, ErrNoToken
	}
	return tok, nil
}

// Put stores a token for the given user and service.
func (b *MemoryBackend) Put(ctx context.Context, req BackendPutRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.tokens[b.storageKey(req.UserID, req.ServiceName)] = req.Token
	return nil
}

// Delete removes the token for the given user and service.
func (b *MemoryBackend) Delete(ctx context.Context, req BackendDeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.tokens, b.storageKey(req.UserID, req.ServiceName))
	return nil
}
