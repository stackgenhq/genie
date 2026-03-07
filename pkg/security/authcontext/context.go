package authcontext

import (
	"context"
)

// Principal represents the authenticated user or entity interacting with Genie.
type Principal struct {
	// ID is a unique identifier for the user (e.g. Email, Username, API Key abbreviation, or "demo-user")
	ID string
	// Name is a human-readable display name, if available.
	Name string
	// Role defines the access tier of the user (e.g. "admin", "user", "agent", "demo")
	Role string
	// AuthenticatedVia describes which strategy verified this principal ("oidc", "jwt", "apikey", "password", "none")
	AuthenticatedVia string
}

// principalContextKey is an unexported type for context keys defined in this package.
// This prevents collisions with keys defined in other packages.
type principalContextKey struct{}

// WithPrincipal returns a new context with the assigned Principal.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

// GetPrincipal retrieves the Principal from the context.
// If no principal is found (meaning authentication was completely disabled or misconfigured),
// it returns a safe "demo" user to ensure the rest of the chain doesn't panic on nil.
func GetPrincipal(ctx context.Context) Principal {
	val := ctx.Value(principalContextKey{})
	if p, ok := val.(Principal); ok {
		return p
	}
	return Principal{
		ID:               "demo-user",
		Name:             "Demo User",
		Role:             "demo",
		AuthenticatedVia: "none",
	}
}

// IsDemo returns true if this principal is the default unauthenticated Demo User.
func (p Principal) IsDemo() bool {
	return p.Role == "demo"
}
