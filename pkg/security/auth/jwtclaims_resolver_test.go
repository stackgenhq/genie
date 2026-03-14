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

var _ = Describe("JWTClaimsResolver", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Resolve", func() {
		It("passes through unchanged if not authenticated via jwt", func() {
			rc := auth.ResolverConfig{
				Resolvers: []auth.ResolverEntry{{Type: "jwt_claims"}},
			}
			resolver := rc.Build()

			sender := identity.Sender{ID: "user1", AuthenticatedVia: "password"}
			eu, err := resolver.Resolve(ctx, auth.ResolveRequest{Sender: sender})

			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Sender).To(Equal(sender))
			Expect(eu.Role).To(BeEmpty())
		})

		It("passes through if no claims in context", func() {
			rc := auth.ResolverConfig{
				Resolvers: []auth.ResolverEntry{{Type: "jwt_claims"}},
			}
			resolver := rc.Build()

			sender := identity.Sender{ID: "user1", AuthenticatedVia: "jwt"}
			// ctx does not have WithClaims
			eu, err := resolver.Resolve(ctx, auth.ResolveRequest{Sender: sender})

			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Sender).To(Equal(sender))
			Expect(eu.Role).To(BeEmpty())
		})

		Context("with default claim names", func() {
			var resolver auth.IdentityResolver

			BeforeEach(func() {
				rc := auth.ResolverConfig{
					Resolvers: []auth.ResolverEntry{{Type: "jwt_claims"}},
				}
				resolver = rc.Build()
			})

			It("extracts roles, groups, and department", func() {
				claims := map[string]any{
					"roles":      "admin",
					"groups":     []any{"infra", "dev"},
					"department": "engineering",
				}
				ctx = auth.WithClaims(ctx, claims)

				sender := identity.Sender{ID: "user1", AuthenticatedVia: "jwt"}
				eu, err := resolver.Resolve(ctx, auth.ResolveRequest{Sender: sender})

				Expect(err).NotTo(HaveOccurred())
				Expect(eu.Role).To(Equal("admin"))
				Expect(eu.Groups).To(ConsistOf("infra", "dev"))
				Expect(eu.Department).To(Equal("engineering"))
			})
		})

		Context("with custom claim names", func() {
			var resolver auth.IdentityResolver

			BeforeEach(func() {
				rc := auth.ResolverConfig{
					Resolvers: []auth.ResolverEntry{
						{
							Type: "jwt_claims",
							Config: map[string]string{
								"role_claim":   "custom_role",
								"groups_claim": "custom_groups",
								"dept_claim":   "custom_dept",
							},
						},
					},
				}
				resolver = rc.Build()
			})

			It("extracts from the custom configured keys", func() {
				claims := map[string]any{
					"roles":         "wrong_role", // Should be ignored
					"custom_role":   "superadmin",
					"custom_groups": []string{"sec", "ops"},
					"custom_dept":   "security",
				}
				ctx = auth.WithClaims(ctx, claims)

				sender := identity.Sender{ID: "user1", AuthenticatedVia: "jwt"}
				eu, err := resolver.Resolve(ctx, auth.ResolveRequest{Sender: sender})

				Expect(err).NotTo(HaveOccurred())
				Expect(eu.Role).To(Equal("superadmin"))
				Expect(eu.Groups).To(ConsistOf("sec", "ops"))
				Expect(eu.Department).To(Equal("security"))
			})
		})
	})
})

var _ = Describe("JWTClaimsResolver Data Type Extraction", func() {
	var ctx context.Context
	var resolver auth.IdentityResolver

	BeforeEach(func() {
		ctx = context.Background()
		rc := auth.ResolverConfig{
			Resolvers: []auth.ResolverEntry{{Type: "jwt_claims"}},
		}
		resolver = rc.Build()
	})

	type typeTestCase struct {
		claims     map[string]any
		wantRole   string
		wantGroups []string
	}

	DescribeTable("handles different JSON unmarshal types",
		func(tc typeTestCase) {
			ctx = auth.WithClaims(ctx, tc.claims)
			sender := identity.Sender{ID: "u", AuthenticatedVia: "jwt"}
			eu, err := resolver.Resolve(ctx, auth.ResolveRequest{Sender: sender})

			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Role).To(Equal(tc.wantRole))
			if len(tc.wantGroups) > 0 {
				Expect(eu.Groups).To(ConsistOf(tc.wantGroups))
			} else {
				Expect(eu.Groups).To(BeEmpty())
			}
		},
		Entry("strings for both", typeTestCase{
			claims:     map[string]any{"roles": "user", "groups": "all-users"},
			wantRole:   "user",
			wantGroups: []string{"all-users"},
		}),
		Entry("[]any (from standard json unmarshal)", typeTestCase{
			claims:     map[string]any{"roles": []any{"admin", "user"}, "groups": []any{"g1", "g2"}},
			wantRole:   "admin", // claimString takes first element
			wantGroups: []string{"g1", "g2"},
		}),
		Entry("[]string (native go type)", typeTestCase{
			claims:     map[string]any{"roles": []string{"mgr"}, "groups": []string{"managers"}},
			wantRole:   "mgr",
			wantGroups: []string{"managers"},
		}),
		Entry("mixed types inside []any skipped", typeTestCase{
			claims:     map[string]any{"groups": []any{"valid", 123, true, "also-valid"}},
			wantRole:   "",
			wantGroups: []string{"valid", "also-valid"},
		}),
	)
})
