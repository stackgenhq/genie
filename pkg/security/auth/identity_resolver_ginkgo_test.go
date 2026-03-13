// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/identity"
	"github.com/stackgenhq/genie/pkg/security/auth"
)

var _ = Describe("IdentityResolver", func() {
	Context("NoopResolver", func() {
		It("passes the sender through unchanged", func() {
			resolver := auth.NewNoopResolver()
			sender := identity.Sender{ID: "test-user"}
			req := auth.ResolveRequest{Sender: sender}

			user, err := resolver.Resolve(context.Background(), req)

			Expect(err).NotTo(HaveOccurred())
			Expect(user.Sender).To(Equal(sender))
			Expect(user.Department).To(BeEmpty())
			Expect(user.Groups).To(BeEmpty())
			Expect(user.Attributes).To(BeEmpty())
		})
	})

	Context("Claims Context Helpers", func() {
		It("stores and retrieves claims from context", func() {
			ctx := context.Background()

			// Initially, GetClaims should return nil
			Expect(auth.GetClaims(ctx)).To(BeNil())

			// Add claims and verify they can be retrieved
			claims := map[string]any{
				"roles": []string{"admin", "user"},
				"iat":   1234567890,
			}
			ctxWithClaims := auth.WithClaims(ctx, claims)

			retrievedClaims := auth.GetClaims(ctxWithClaims)
			Expect(retrievedClaims).NotTo(BeNil())
			Expect(retrievedClaims).To(Equal(claims))
		})
	})

	Context("StaticResolver", func() {
		var resolver auth.IdentityResolver
		ctx := context.Background()

		BeforeEach(func() {
			cfg := map[string]string{
				"john@acme.com":   "role:admin,groups:infra|dev,dept:engineering",
				"tester@acme.com": "role:user, groups: qa, ",
				"bad@acme.com":    "invalid_format",
			}

			rc := auth.ResolverConfig{
				Resolvers: []auth.ResolverEntry{
					{
						Type:   "static",
						Config: cfg,
					},
				},
			}
			resolver = rc.Build()
		})

		It("resolves a fully configured static identity", func() {
			req := auth.ResolveRequest{Sender: identity.Sender{ID: "john@acme.com"}}
			eu, err := resolver.Resolve(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Sender.ID).To(Equal("john@acme.com"))
			Expect(eu.Role).To(Equal("admin"))
			Expect(eu.Department).To(Equal("engineering"))
			Expect(eu.Groups).To(ConsistOf("infra", "dev"))
		})

		It("resolves with whitespace-trimmed config", func() {
			req := auth.ResolveRequest{Sender: identity.Sender{ID: "tester@acme.com"}}
			eu, err := resolver.Resolve(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Sender.ID).To(Equal("tester@acme.com"))
			Expect(eu.Role).To(Equal("user"))
			Expect(eu.Department).To(BeEmpty())
			Expect(eu.Groups).To(ConsistOf("qa"))
		})

		It("gracefully falls back on invalid formats", func() {
			req := auth.ResolveRequest{Sender: identity.Sender{ID: "bad@acme.com"}}
			eu, err := resolver.Resolve(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Role).To(BeEmpty()) // Didn't parse, defaults remain
		})

		It("passes unknown users through unchanged", func() {
			req := auth.ResolveRequest{Sender: identity.Sender{ID: "nobody@acme.com"}}
			eu, err := resolver.Resolve(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Sender.ID).To(Equal("nobody@acme.com"))
			Expect(eu.Role).To(BeEmpty())
		})
	})
})
