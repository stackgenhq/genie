// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
)

const (
	defaultRoleClaim   = "roles"
	defaultGroupsClaim = "groups"
	defaultDeptClaim   = "department"
)

// jwtClaimsResolver extracts user roles and group memberships from verified JWT
// claims stored in the request context. Claim keys are configurable to support
// different IdP token formats (Okta, Auth0, Keycloak, Azure AD, etc.).
//
// It reads the raw claims map stored by WithClaims (populated by the JWT
// auth middleware after signature verification).
type jwtClaimsResolver struct {
	roleClaim    string
	groupsClaim  string
	deptClaim    string
	groupRoleMap map[string]string // group name (lowercase) → role
}

// newJWTClaimsResolver creates a jwtClaimsResolver with defaults for claim names.
// Custom claim names may be supplied via the ResolverEntry.Config map:
//   - "role_claim"    → which JWT claim holds the role string (default: "roles")
//   - "groups_claim"  → which JWT claim holds the groups array (default: "groups")
//   - "dept_claim"    → which JWT claim holds the department (default: "department")
func newJWTClaimsResolver(cfg map[string]string) *jwtClaimsResolver {
	r := &jwtClaimsResolver{
		roleClaim:   defaultRoleClaim,
		groupsClaim: defaultGroupsClaim,
		deptClaim:   defaultDeptClaim,
	}
	if v, ok := cfg["role_claim"]; ok && v != "" {
		r.roleClaim = v
	}
	if v, ok := cfg["groups_claim"]; ok && v != "" {
		r.groupsClaim = v
	}
	if v, ok := cfg["dept_claim"]; ok && v != "" {
		r.deptClaim = v
	}
	return r
}

// Resolve reads JWT claims from the context and enriches the sender's identity.
// It only acts when the sender was authenticated via JWT; all other auth methods
// are passed through unchanged.
func (j *jwtClaimsResolver) Resolve(ctx context.Context, req ResolveRequest) (EnterpriseUser, error) {
	logr := logger.GetLogger(ctx).With("fn", "jwtClaimsResolver.Resolve", "user_id", req.Sender.ID)

	// Only enrich JWT-authenticated senders.
	if req.Sender.AuthenticatedVia != "jwt" {
		return EnterpriseUser{Sender: req.Sender}, nil
	}

	claims := GetClaims(ctx)
	if claims == nil {
		logr.Debug("no JWT claims in context, passing through")
		return EnterpriseUser{Sender: req.Sender}, nil
	}

	return j.resolveFromClaims(ctx, req, claims), nil
}

// resolveFromClaims extracts role, groups, and department from the parsed claims map.
func (j *jwtClaimsResolver) resolveFromClaims(ctx context.Context, req ResolveRequest, claims map[string]any) EnterpriseUser {
	logr := logger.GetLogger(ctx).With("fn", "jwtClaimsResolver.resolveFromClaims", "user_id", req.Sender.ID)
	sender := req.Sender

	// Extract role.
	if role := claimString(claims, j.roleClaim); role != "" {
		sender.Role = role
		logr.Debug("resolved role from JWT claim", "claim", j.roleClaim, "role", role)
	}

	// Extract groups.
	groups := claimStringSlice(claims, j.groupsClaim)

	// Extract department.
	department := claimString(claims, j.deptClaim)

	// If no role yet, attempt group → role mapping.
	if sender.Role == "" && len(j.groupRoleMap) > 0 {
		for _, g := range groups {
			if role, ok := j.groupRoleMap[strings.ToLower(g)]; ok {
				sender.Role = role
				logr.Debug("resolved role from group mapping", "group", g, "role", role)
				break
			}
		}
	}

	return EnterpriseUser{
		Sender:     sender,
		Groups:     groups,
		Department: department,
	}
}

// claimString extracts a single string value from a claims map.
// Handles string and []any (takes first element) formats.
func claimString(claims map[string]any, key string) string {
	val, ok := claims[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	case []string:
		if len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// claimStringSlice extracts a []string from a claims map.
// Handles []string and []any per OIDC spec.
func claimStringSlice(claims map[string]any, key string) []string {
	val, ok := claims[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		return []string{v}
	}
	return nil
}
