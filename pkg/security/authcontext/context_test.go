package authcontext_test

import (
	"context"
	"testing"

	"github.com/stackgenhq/genie/pkg/security/authcontext"
)

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()

	// Before injection, it should return a safe unauthenticated default
	p1 := authcontext.GetPrincipal(ctx)
	if p1.ID != "demo-user" {
		t.Errorf("expected demo-user ID initially, got %s", p1.ID)
	}
	if p1.AuthenticatedVia != "none" {
		t.Errorf("expected no auth initially, got %s", p1.AuthenticatedVia)
	}

	// Inject realistic OIDC token user
	expected := authcontext.Principal{
		ID:               "alice@stackgen.com",
		Name:             "Alice",
		Role:             "user",
		AuthenticatedVia: "oidc",
	}

	ctxUser := authcontext.WithPrincipal(ctx, expected)
	p2 := authcontext.GetPrincipal(ctxUser)
	if p2.ID != "alice@stackgen.com" {
		t.Errorf("expected alice ID after injection, got %s", p2.ID)
	}
	if p2.AuthenticatedVia != "oidc" {
		t.Errorf("expected oidc auth after injection, got %s", p2.AuthenticatedVia)
	}

	// Ensure the base context wasn't accidentally mutated (in Go it technically can't be, but sanity check)
	p3 := authcontext.GetPrincipal(ctx)
	if p3.ID != "demo-user" {
		t.Errorf("expected demo-user on old context, got %s", p3.ID)
	}
}

func TestIsDemo(t *testing.T) {
	demo := authcontext.GetPrincipal(context.Background())
	if !demo.IsDemo() {
		t.Errorf("expected unauthenticated root user to report as demo user")
	}

	real := authcontext.Principal{
		ID:               "system",
		Role:             "agent",
		AuthenticatedVia: "apikey",
	}
	if real.IsDemo() {
		t.Errorf("expected explicit agent user to NOT report as demo user")
	}
}
