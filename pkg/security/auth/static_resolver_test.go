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

var _ = Describe("StaticResolver", func() {
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

	type staticTestCase struct {
		userID     string
		wantRole   string
		wantDept   string
		wantGroups []string
	}

	DescribeTable("Resolving static identities",
		func(tc staticTestCase) {
			req := auth.ResolveRequest{Sender: identity.Sender{ID: tc.userID}}
			eu, err := resolver.Resolve(ctx, req)

			Expect(err).NotTo(HaveOccurred())
			Expect(eu.Sender.ID).To(Equal(tc.userID))
			Expect(eu.Role).To(Equal(tc.wantRole))
			Expect(eu.Department).To(Equal(tc.wantDept))
			if len(tc.wantGroups) > 0 {
				Expect(eu.Groups).To(ConsistOf(tc.wantGroups))
			} else {
				Expect(eu.Groups).To(BeEmpty())
			}
		},
		Entry("valid full user", staticTestCase{
			userID:     "john@acme.com",
			wantRole:   "admin",
			wantDept:   "engineering",
			wantGroups: []string{"infra", "dev"},
		}),
		Entry("whitespace trimmed user", staticTestCase{
			userID:     "tester@acme.com",
			wantRole:   "user",
			wantDept:   "",
			wantGroups: []string{"qa"},
		}),
		Entry("invalid format gracefully continues", staticTestCase{
			userID:     "bad@acme.com",
			wantRole:   "",
			wantDept:   "",
			wantGroups: nil,
		}),
		Entry("unknown user gets passed through", staticTestCase{
			userID:     "nobody@acme.com",
			wantRole:   "",
			wantDept:   "",
			wantGroups: nil,
		}),
	)
})
