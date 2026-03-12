// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package rbac provides lightweight role-based access control for tool
// authorization. It checks the identity.Sender from context to determine
// whether the current user has sufficient privileges.
package rbac

import (
	"context"

	"github.com/stackgenhq/genie/pkg/identity"
)

// Config holds RBAC settings loaded from the application configuration.
type Config struct {
	// AdminUsers is a list of user IDs (email, username, etc.) that are
	// granted the admin role regardless of their auth-provided role.
	// Example: ["alice@company.com", "bob@company.com"]
	AdminUsers []string `yaml:"admin_users,omitempty" toml:"admin_users,omitempty"`
}

// RBAC evaluates role-based access using the identity.Sender from context.
type RBAC struct {
	adminSet map[string]struct{}
}

// New creates an RBAC instance from the given config.
func New(cfg Config) *RBAC {
	adminSet := make(map[string]struct{}, len(cfg.AdminUsers))
	for _, id := range cfg.AdminUsers {
		adminSet[id] = struct{}{}
	}
	return &RBAC{adminSet: adminSet}
}

// IsAdmin returns true if the current user (from context) is allowed to
// perform admin-level operations. A user is considered admin when:
//  1. Their identity.Sender.Role is "admin", OR
//  2. Their identity.Sender is the demo user (no auth configured), OR
//  3. Their identity.Sender.ID is in the configured AdminUsers list.
func (r *RBAC) IsAdmin(ctx context.Context) bool {
	sender := identity.GetSender(ctx)

	// Demo user (no auth) gets full access for local/dev usage.
	if sender.IsDemo() {
		return true
	}

	// Explicit admin role from auth provider.
	if sender.Role == "admin" {
		return true
	}

	// Configured admin user list.
	if _, ok := r.adminSet[sender.ID]; ok {
		return true
	}

	return false
}
