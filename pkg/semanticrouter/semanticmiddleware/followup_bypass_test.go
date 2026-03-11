// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticmiddleware_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/semanticrouter/semanticmiddleware"
)

var _ = Describe("FollowUp Bypass Middleware", func() {
	var (
		ctx        context.Context
		next       semanticmiddleware.ClassifyFunc
		nextCalled bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		nextCalled = false
		next = func(_ context.Context, _ *semanticmiddleware.ClassifyContext) (semanticmiddleware.ClassifyResult, error) {
			nextCalled = true
			return semanticmiddleware.ClassifyResult{Category: "TERMINAL", Level: "next"}, nil
		}
	})

	Describe("when IsFollowUp is true", func() {
		It("should short-circuit to COMPLEX", func() {
			mw := semanticmiddleware.NewFollowUpBypass(semanticmiddleware.FollowUpBypassConfig{})
			cc := &semanticmiddleware.ClassifyContext{
				Question:   "try again",
				IsFollowUp: true,
			}

			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("COMPLEX"))
			Expect(res.BypassedLLM).To(BeTrue())
			Expect(res.Level).To(Equal("follow_up_bypass"))
			Expect(nextCalled).To(BeFalse())
		})
	})

	Describe("when IsFollowUp is false", func() {
		It("should pass through to next", func() {
			mw := semanticmiddleware.NewFollowUpBypass(semanticmiddleware.FollowUpBypassConfig{})
			cc := &semanticmiddleware.ClassifyContext{
				Question:   "deploy my app",
				IsFollowUp: false,
			}

			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
			Expect(nextCalled).To(BeTrue())
		})
	})

	Describe("with Disabled=true", func() {
		It("should pass through to next even when IsFollowUp is true", func() {
			mw := semanticmiddleware.NewFollowUpBypass(semanticmiddleware.FollowUpBypassConfig{
				Disabled: true,
			})
			cc := &semanticmiddleware.ClassifyContext{
				Question:   "try again",
				IsFollowUp: true,
			}

			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
			Expect(nextCalled).To(BeTrue())
		})
	})
})
