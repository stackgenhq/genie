// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/identity"
)

func TestRBAC(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "RBAC Suite")
}

var _ = Describe("RBAC", func() {
	Describe("IsAdmin", func() {
		It("should allow demo user (no auth)", func() {
			r := New(Config{})
			ctx := context.Background() // no sender = DemoSender
			Expect(r.IsAdmin(ctx)).To(BeTrue())
		})

		It("should allow users with admin role", func() {
			r := New(Config{})
			ctx := identity.WithSender(context.Background(), identity.Sender{
				ID:   "alice@co.com",
				Role: "admin",
			})
			Expect(r.IsAdmin(ctx)).To(BeTrue())
		})

		It("should allow users in AdminUsers list", func() {
			r := New(Config{AdminUsers: []string{"bob@co.com"}})
			ctx := identity.WithSender(context.Background(), identity.Sender{
				ID:   "bob@co.com",
				Role: "user",
			})
			Expect(r.IsAdmin(ctx)).To(BeTrue())
		})

		It("should deny regular users not in admin list", func() {
			r := New(Config{AdminUsers: []string{"bob@co.com"}})
			ctx := identity.WithSender(context.Background(), identity.Sender{
				ID:   "charlie@co.com",
				Role: "user",
			})
			Expect(r.IsAdmin(ctx)).To(BeFalse())
		})

		It("should deny users with empty role and not in list", func() {
			r := New(Config{})
			ctx := identity.WithSender(context.Background(), identity.Sender{
				ID:   "nobody@co.com",
				Role: "user",
			})
			Expect(r.IsAdmin(ctx)).To(BeFalse())
		})
	})
})
