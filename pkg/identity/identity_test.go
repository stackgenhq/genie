// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package identity_test

import (
	"context"
	"testing"

	"github.com/stackgenhq/genie/pkg/identity"
)

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()

	s1 := identity.GetSender(ctx)
	if s1.ID != "demo-user" {
		t.Errorf("expected demo-user ID initially, got %s", s1.ID)
	}
	if s1.AuthenticatedVia != "none" {
		t.Errorf("expected no auth initially, got %s", s1.AuthenticatedVia)
	}

	expected := identity.Sender{
		ID:               "alice@stackgen.com",
		DisplayName:      "Alice",
		Role:             "user",
		AuthenticatedVia: "oidc",
	}

	ctxUser := identity.WithSender(ctx, expected)
	s2 := identity.GetSender(ctxUser)
	if s2.ID != "alice@stackgen.com" {
		t.Errorf("expected alice ID after injection, got %s", s2.ID)
	}
	if s2.AuthenticatedVia != "oidc" {
		t.Errorf("expected oidc auth after injection, got %s", s2.AuthenticatedVia)
	}

	s3 := identity.GetSender(ctx)
	if s3.ID != "demo-user" {
		t.Errorf("expected demo-user on old context, got %s", s3.ID)
	}
}

func TestIsDemo(t *testing.T) {
	demo := identity.GetSender(context.Background())
	if !demo.IsDemo() {
		t.Errorf("expected unauthenticated root user to report as demo user")
	}

	real := identity.Sender{
		ID:               "system",
		Role:             "agent",
		AuthenticatedVia: "apikey",
	}
	if real.IsDemo() {
		t.Errorf("expected explicit agent user to NOT report as demo user")
	}
}
