// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import "context"

// claimsContextKey is an unexported type for the JWT claims context key.
type claimsContextKey struct{}

// WithClaims returns a new context containing the raw JWT claims map.
// This is called by the auth middleware after parsing the JWT so that
// downstream resolvers can read custom claims like "roles" and "groups".
func WithClaims(ctx context.Context, claims map[string]any) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// GetClaims retrieves the raw JWT claims from the context.
// Returns nil if no claims are present (e.g. non-JWT auth, messenger path).
func GetClaims(ctx context.Context) map[string]any {
	val := ctx.Value(claimsContextKey{})
	if c, ok := val.(map[string]any); ok {
		return c
	}
	return nil
}
