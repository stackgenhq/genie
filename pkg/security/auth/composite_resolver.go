// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"

	"github.com/stackgenhq/genie/pkg/logger"
)

// compositeResolver chains multiple IdentityResolver implementations.
// The first resolver to return a non-empty Role wins.
// If a resolver returns an error, it is logged and the next is tried.
//
// Sender propagation: after each successful resolve, the enriched
// Sender is passed forward to subsequent resolvers via req.Sender.
// The original platform user ID is preserved in req.PlatformUserID so
// platform-specific resolvers can still match the original identity.
type compositeResolver struct {
	resolvers []IdentityResolver
}

// newCompositeResolver creates a compositeResolver that tries each resolver in order.
func newCompositeResolver(resolvers ...IdentityResolver) *compositeResolver {
	return &compositeResolver{resolvers: resolvers}
}

// Resolve iterates through each resolver in order. The first resolver
// that returns a non-empty Role wins. Falls back gracefully to the last
// successful result if no resolver assigns a role.
func (c *compositeResolver) Resolve(ctx context.Context, req ResolveRequest) (EnterpriseUser, error) {
	logr := logger.GetLogger(ctx).With("fn", "compositeResolver.Resolve", "user_id", req.Sender.ID)

	// Preserve the original platform user ID before any resolver rewrites Sender.ID.
	if req.PlatformUserID == "" {
		req.PlatformUserID = req.Sender.ID
	}

	var lastResult EnterpriseUser
	for i, r := range c.resolvers {
		eu, err := r.Resolve(ctx, req)
		if err != nil {
			logr.Warn("resolver failed, trying next",
				"resolver_index", i,
				"error", err,
			)
			continue
		}

		lastResult = eu
		// Propagate the enriched sender to the next resolver.
		req.Sender = eu.Sender

		if eu.Role != "" {
			logr.Debug("resolved role", "resolver_index", i, "role", eu.Role)
			return eu, nil
		}
	}

	// No resolver assigned a role — return the last result (graceful passthrough).
	return lastResult, nil
}
