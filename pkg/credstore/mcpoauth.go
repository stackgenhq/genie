// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"

	"golang.org/x/oauth2"
)

// mcpOAuthStore implements Store using mcp-go's OAuthHandler, which supports
// Dynamic Client Registration (RFC 7591) as specified by the MCP authorization
// spec (2025-03-26). This is the preferred mode for MCP servers that expose
// a /.well-known/oauth-protected-resource or /register endpoint.
//
// Flow:
//  1. On first GetToken, discovers server metadata from the MCP server URL
//  2. If no client_id exists, performs Dynamic Client Registration
//  3. Builds authorization URL and returns AuthRequiredError
//  4. User clicks link → browser OAuth → callback
//  5. ProcessAuthorizationResponse exchanges code for token
//  6. Token stored per-user via Backend
type mcpOAuthStore struct {
	serviceName string
	backend     Backend
	handler     *transport.OAuthHandler
	pending     *PendingAuthStore
}

// MCPOAuthConfig holds configuration for an MCP OAuth store with DCR.
type MCPOAuthConfig struct {
	// ServerURL is the MCP server URL (e.g. http://poc.cloud.stackgen.com/api/mcp/sse).
	// Used to discover /.well-known/oauth-protected-resource and registration endpoint.
	ServerURL string
	// RedirectURI is the OAuth callback URL (e.g. https://your-server.com/oauth/callback).
	RedirectURI string
	// ClientName is the name used during dynamic client registration (e.g. "Genie Agent").
	ClientName string
	// ClientID is an optional pre-configured OAuth client ID.
	// If provided, Dynamic Client Registration is skipped.
	ClientID string
	// ClientSecret is an optional pre-configured OAuth client secret.
	// Used only if ClientID is also provided.
	ClientSecret string
	// Scopes is an optional list of OAuth scopes to request.
	Scopes []string
}

// NewMCPOAuthStoreRequest is the request for NewMCPOAuthStore.
type NewMCPOAuthStoreRequest struct {
	ServiceName string
	Backend     Backend
	Config      MCPOAuthConfig
	Pending     *PendingAuthStore
}

// NewMCPOAuthStore creates a Store that uses mcp-go's OAuthHandler with
// Dynamic Client Registration. No pre-configured client_id or client_secret
// is needed — they are obtained automatically from the MCP server's
// registration endpoint.
func NewMCPOAuthStore(req NewMCPOAuthStoreRequest) Store {
	oauthHandler := transport.NewOAuthHandler(transport.OAuthConfig{
		RedirectURI:  req.Config.RedirectURI,
		ClientID:     req.Config.ClientID,
		ClientSecret: req.Config.ClientSecret,
		Scopes:       req.Config.Scopes,
		PKCEEnabled:  true, // Always use PKCE for public clients with DCR
	})
	// Set the base URL so mcp-go can discover server metadata.
	// We extract just the scheme and host because .well-known endpoints
	// are typically located at the root of the domain (e.g., https://example.com/.well-known/...)
	// rather than relative to the SSE path.
	if parsed, err := url.Parse(req.Config.ServerURL); err == nil && parsed.Host != "" {
		oauthHandler.SetBaseURL(fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host))
	} else {
		oauthHandler.SetBaseURL(req.Config.ServerURL)
	}

	return &mcpOAuthStore{
		serviceName: req.ServiceName,
		backend:     req.Backend,
		handler:     oauthHandler,
		pending:     req.Pending,
	}
}

// GetToken returns the token for the current user. If no token exists,
// performs dynamic client registration (if needed) and returns an
// AuthRequiredError with the authorization URL.
func (s *mcpOAuthStore) GetToken(ctx context.Context) (*Token, error) {
	userID := userIDFromContext(ctx)

	tok, err := s.backend.Get(ctx, BackendGetRequest{
		UserID:      userID,
		ServiceName: s.serviceName,
	})
	if err == nil && tok != nil && !tok.IsExpired() && tok.AccessToken != "" {
		return tok, nil
	}

	// If we have a refresh token, try refreshing via mcp-go's handler.
	if err == nil && tok != nil && tok.RefreshToken != "" {
		refreshed, refreshErr := s.handler.RefreshToken(ctx, tok.RefreshToken)
		if refreshErr == nil {
			result := tokenFromMCPToken(refreshed)
			_ = s.backend.Put(ctx, BackendPutRequest{
				UserID: userID, ServiceName: s.serviceName, Token: result,
			})
			return result, nil
		}
		// Refresh failed — fall through to re-auth.
	}

	// If no client ID yet, perform Dynamic Client Registration.
	if s.handler.GetClientID() == "" {
		if regErr := s.handler.RegisterClient(ctx, s.serviceName); regErr != nil {
			return nil, fmt.Errorf("credstore: dynamic client registration failed for %s: %w", s.serviceName, regErr)
		}
	}

	// Build auth URL via mcp-go's OAuthHandler.
	state := oauth2.GenerateVerifier()
	codeVerifier := oauth2.GenerateVerifier()
	codeChallenge := oauth2.S256ChallengeOption(codeVerifier)
	// We need the raw challenge string for the URL. Let's compute it.
	challengeStr := computeS256Challenge(codeVerifier)

	authURL, err := s.handler.GetAuthorizationURL(ctx, state, challengeStr)
	if err != nil {
		return nil, fmt.Errorf("credstore: failed to get auth URL for %s: %w", s.serviceName, err)
	}

	// Store pending auth state.
	s.pending.Store(state, PendingAuth{
		UserID:       userID,
		ServiceName:  s.serviceName,
		CodeVerifier: codeVerifier,
		ExpiresAt:    time.Now().Add(pendingAuthTTL),
	})

	// Suppress unused variable (codeChallenge is used for type assertion only).
	_ = codeChallenge

	return nil, &AuthRequiredError{
		AuthURL:     authURL,
		ServiceName: s.serviceName,
	}
}

// SaveToken stores a token for the current user.
func (s *mcpOAuthStore) SaveToken(ctx context.Context, token *Token) error {
	userID := userIDFromContext(ctx)
	return s.backend.Put(ctx, BackendPutRequest{
		UserID: userID, ServiceName: s.serviceName, Token: token,
	})
}

// DeleteToken removes the token for the current user.
func (s *mcpOAuthStore) DeleteToken(ctx context.Context) error {
	userID := userIDFromContext(ctx)
	return s.backend.Delete(ctx, BackendDeleteRequest{
		UserID: userID, ServiceName: s.serviceName,
	})
}

// ServiceName returns the service name.
func (s *mcpOAuthStore) ServiceName() string {
	return s.serviceName
}

// tokenFromMCPToken converts an mcp-go Token to our Token type.
func tokenFromMCPToken(t *transport.Token) *Token {
	return &Token{
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
	}
}

// computeS256Challenge computes the S256 PKCE code challenge from a verifier.
func computeS256Challenge(verifier string) string {
	// Use crypto/sha256 + base64url encoding per RFC 7636.
	//nolint:gosec // Not a crypto secret, just a PKCE challenge
	h := sha256Sum([]byte(verifier))
	return base64URLEncode(h[:])
}
