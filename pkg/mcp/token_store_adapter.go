// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/stackgenhq/genie/pkg/credstore"
)

// tokenStoreAdapter wraps a credstore.Store to implement transport.TokenStore.
// It bridges the gap between our internal credstore types and the mcp-go types.
type tokenStoreAdapter struct {
	store credstore.Store
}

// newTokenStoreAdapter creates a new token store adapter.
func newTokenStoreAdapter(store credstore.Store) *tokenStoreAdapter {
	if store == nil {
		return nil
	}
	return &tokenStoreAdapter{store: store}
}

// GetToken returns the current token, converted to transport.Token.
func (a *tokenStoreAdapter) GetToken(ctx context.Context) (*transport.Token, error) {
	tok, err := a.store.GetToken(ctx)
	if err != nil {
		// Map our internal ErrNoToken to the mcp-go ErrNoToken so the OAuthHandler knows.
		if err == credstore.ErrNoToken {
			return nil, transport.ErrNoToken
		}
		// Also map auth required errors so the handler triggers registration/auth.
		if _, ok := err.(*credstore.AuthRequiredError); ok {
			return nil, transport.ErrNoToken
		}
		return nil, err
	}

	return &transport.Token{
		AccessToken:  tok.AccessToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
		ExpiresAt:    tok.ExpiresAt,
		Scope:        tok.Scope,
	}, nil
}

// SaveToken saves a token, converting from transport.Token to our internal type.
func (a *tokenStoreAdapter) SaveToken(ctx context.Context, token *transport.Token) error {
	if token == nil {
		return nil
	}

	return a.store.SaveToken(ctx, &credstore.Token{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    token.ExpiresIn,
		ExpiresAt:    token.ExpiresAt,
		Scope:        token.Scope,
	})
}
