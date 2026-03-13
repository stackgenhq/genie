// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"

	"github.com/stackgenhq/genie/pkg/identity"
)

// ResolveRequest carries the known sender information from the platform
// adapter or auth middleware. Implementations use this to look up the
// enterprise identity.
type ResolveRequest struct {
	// Sender is the raw identity from the messenger or auth layer.
	// At minimum, Sender.ID is populated.
	Sender identity.Sender

	// Platform is the originating platform (e.g. "slack", "agui",
	// "teams", "telegram"). Implementations may use this to choose
	// different resolution strategies per platform.
	Platform string

	// PlatformUserID is the original platform-specific user identifier
	// (e.g. Slack "U_ABC123", Google OAuth subject).
	//
	// If empty, resolvers should fall back to Sender.ID.
	PlatformUserID string
}

// EnterpriseUser is the resolved identity enriched with enterprise
// metadata. It embeds identity.Sender so it can be used as a drop-in
// replacement wherever a Sender is expected.
type EnterpriseUser struct {
	identity.Sender

	// Department is the organisational department (e.g. "engineering",
	// "hr", "finance"). Empty if unknown.
	Department string

	// Groups lists the group memberships for this user (e.g.
	// ["platform-team", "on-call", "sre"]). May be empty.
	Groups []string

	// Attributes holds arbitrary key-value metadata about the user.
	// Useful for customer-specific fields that don't warrant a
	// dedicated struct field (e.g. "cost_center", "manager_email").
	Attributes map[string]string
}

// IdentityResolver maps an incoming platform sender to an enterprise user.
//
// Implementations may:
//   - Query a local database
//   - Parse JWT/OIDC claims (roles, groups)
//   - Call a directory API (SCIM directory)
//
//counterfeiter:generate . IdentityResolver
type IdentityResolver interface {
	// Resolve looks up the enterprise identity for the given sender.
	// Implementations MUST be safe for concurrent use.
	//
	// If the user cannot be found, implementations should return
	// the original sender unchanged (not an error), so that the
	// system degrades gracefully to unauthenticated behavior.
	Resolve(ctx context.Context, req ResolveRequest) (EnterpriseUser, error)
}
