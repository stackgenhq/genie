// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/security"
)

// staticStore implements Store for pre-configured tokens. The token is
// resolved from a SecretProvider (env var, secrets manager, etc.) and
// is typically shared across all users (org-level PAT or API key).
//
// Example use cases:
//   - GitHub PAT for SCM tools
//   - Slack bot token
//   - Third-party API keys
type staticStore struct {
	serviceName string
	provider    security.SecretProvider
	secretName  string // e.g. "GITHUB_TOKEN"
}

// NewStaticStoreRequest is the request for NewStaticStore.
type NewStaticStoreRequest struct {
	ServiceName string
	Provider    security.SecretProvider
	SecretName  string
}

// NewStaticStore creates a Store backed by a static token from SecretProvider.
// The token is resolved on each GetToken call (not cached), so secret rotation
// is respected automatically.
func NewStaticStore(req NewStaticStoreRequest) Store {
	return &staticStore{
		serviceName: req.ServiceName,
		provider:    req.Provider,
		secretName:  req.SecretName,
	}
}

// GetToken resolves the token from the SecretProvider. The token is treated
// as a Bearer access token with no expiry.
func (s *staticStore) GetToken(ctx context.Context) (*Token, error) {
	val, err := s.provider.GetSecret(ctx, security.GetSecretRequest{
		Name:   s.secretName,
		Reason: fmt.Sprintf("credstore: static token for %s", s.serviceName),
	})
	if err != nil {
		return nil, fmt.Errorf("credstore: failed to resolve secret %s: %w", s.secretName, err)
	}
	if val == "" {
		return nil, ErrNoToken
	}
	return &Token{
		AccessToken: val,
		TokenType:   "Bearer",
	}, nil
}

// SaveToken is a no-op for static stores — the token comes from config.
func (s *staticStore) SaveToken(_ context.Context, _ *Token) error {
	return nil
}

// DeleteToken is a no-op for static stores — the token comes from config.
func (s *staticStore) DeleteToken(_ context.Context) error {
	return nil
}

// ServiceName returns the service name.
func (s *staticStore) ServiceName() string {
	return s.serviceName
}
