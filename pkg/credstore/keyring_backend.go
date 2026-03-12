// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stackgenhq/genie/pkg/security/keyring"
)

// KeyringBackend stores tokens in the system keychain (macOS Keychain,
// Windows Credential Manager, Linux Secret Service). Suitable for
// local, single-user Genie deployments. Falls back to in-memory when
// the system keyring is unavailable (Docker, headless Linux).
type KeyringBackend struct{}

// NewKeyringBackend creates a new keyring-based token backend.
func NewKeyringBackend() *KeyringBackend {
	return &KeyringBackend{}
}

func (b *KeyringBackend) keyringAccount(userID, serviceName string) string {
	return fmt.Sprintf("credstore_%s_%s", serviceName, userID)
}

// Get returns the token for the given user and service from the keyring.
func (b *KeyringBackend) Get(ctx context.Context, req BackendGetRequest) (*Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := keyring.KeyringGet(b.keyringAccount(req.UserID, req.ServiceName))
	if err != nil {
		return nil, ErrNoToken
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("credstore: corrupt token in keyring: %w", err)
	}
	return &tok, nil
}

// Put stores a token for the given user and service in the keyring.
func (b *KeyringBackend) Put(ctx context.Context, req BackendPutRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(req.Token)
	if err != nil {
		return fmt.Errorf("credstore: failed to marshal token: %w", err)
	}
	return keyring.KeyringSet(b.keyringAccount(req.UserID, req.ServiceName), data)
}

// Delete removes the token for the given user and service from the keyring.
func (b *KeyringBackend) Delete(ctx context.Context, req BackendDeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return keyring.KeyringDelete(b.keyringAccount(req.UserID, req.ServiceName))
}
