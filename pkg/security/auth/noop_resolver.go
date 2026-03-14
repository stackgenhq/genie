// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import "context"

// NoopResolver is the default IdentityResolver that passes the sender through
// unchanged. Use it when no enterprise identity provider is configured
// — the system operates in "everyone is who they say they are" mode.
type NoopResolver struct{}

// NewNoopResolver creates a NoopResolver.
func NewNoopResolver() *NoopResolver {
	return &NoopResolver{}
}

// Resolve returns the sender as-is, wrapped in an EnterpriseUser.
func (n *NoopResolver) Resolve(_ context.Context, req ResolveRequest) (EnterpriseUser, error) {
	return EnterpriseUser{Sender: req.Sender}, nil
}
