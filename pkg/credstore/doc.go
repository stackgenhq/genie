// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package credstore provides per-user, per-service credential management for
// MCP servers and third-party integrations. It supports two modes:
//
//   - Static: user provides a token (PAT, API key) via config/env/secrets.
//     The token is resolved via SecretProvider. No OAuth dance needed.
//
//   - OAuth2: full authorization code flow with PKCE. When no token is present,
//     GetToken returns ErrAuthRequired with an authorization URL. The caller
//     (typically the MCP tool adapter) sends this URL to the user via chat.
//     After the user authenticates, the OAuth callback handler exchanges the
//     code for a token and stores it per-user.
//
// User resolution is context-based: MessageOrigin.Sender.ID from the request
// context identifies who is calling. This allows a single Store instance per
// MCP server to manage tokens for many users.
//
// The Store interface matches mcp-go's transport.TokenStore signature, so it
// plugs directly into MCP OAuth clients without adapters.
package credstore
