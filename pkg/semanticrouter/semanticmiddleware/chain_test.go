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

var _ = Describe("BuildChain", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should return COMPLEX from terminal when no middlewares are provided", func() {
		chain := semanticmiddleware.BuildChain()
		cc := &semanticmiddleware.ClassifyContext{Question: "hello"}

		res, err := chain(ctx, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Category).To(Equal("COMPLEX"))
		Expect(res.Level).To(Equal("terminal"))
	})

	It("should execute middlewares in order", func() {
		var order []string
		mw1 := func(ctx context.Context, cc *semanticmiddleware.ClassifyContext, next semanticmiddleware.ClassifyFunc) (semanticmiddleware.ClassifyResult, error) {
			order = append(order, "mw1")
			return next(ctx, cc)
		}
		mw2 := func(ctx context.Context, cc *semanticmiddleware.ClassifyContext, next semanticmiddleware.ClassifyFunc) (semanticmiddleware.ClassifyResult, error) {
			order = append(order, "mw2")
			return next(ctx, cc)
		}

		chain := semanticmiddleware.BuildChain(mw1, mw2)
		cc := &semanticmiddleware.ClassifyContext{Question: "test"}
		_, err := chain(ctx, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(order).To(Equal([]string{"mw1", "mw2"}))
	})

	It("should allow a middleware to short-circuit", func() {
		mw1 := func(ctx context.Context, cc *semanticmiddleware.ClassifyContext, _ semanticmiddleware.ClassifyFunc) (semanticmiddleware.ClassifyResult, error) {
			return semanticmiddleware.ClassifyResult{Category: "REFUSE", Level: "mw1"}, nil
		}
		mw2 := func(_ context.Context, _ *semanticmiddleware.ClassifyContext, _ semanticmiddleware.ClassifyFunc) (semanticmiddleware.ClassifyResult, error) {
			Fail("mw2 should not be called")
			return semanticmiddleware.ClassifyResult{}, nil
		}

		chain := semanticmiddleware.BuildChain(mw1, mw2)
		cc := &semanticmiddleware.ClassifyContext{Question: "hack"}
		res, err := chain(ctx, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Category).To(Equal("REFUSE"))
		Expect(res.Level).To(Equal("mw1"))
	})

	It("should allow middlewares to enrich context for downstream", func() {
		mw1 := func(ctx context.Context, cc *semanticmiddleware.ClassifyContext, next semanticmiddleware.ClassifyFunc) (semanticmiddleware.ClassifyResult, error) {
			cc.IsFollowUp = true
			return next(ctx, cc)
		}
		mw2 := func(ctx context.Context, cc *semanticmiddleware.ClassifyContext, next semanticmiddleware.ClassifyFunc) (semanticmiddleware.ClassifyResult, error) {
			if cc.IsFollowUp {
				return semanticmiddleware.ClassifyResult{Category: "COMPLEX", Level: "mw2"}, nil
			}
			return next(ctx, cc)
		}

		chain := semanticmiddleware.BuildChain(mw1, mw2)
		cc := &semanticmiddleware.ClassifyContext{Question: "try again"}
		res, err := chain(ctx, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Category).To(Equal("COMPLEX"))
		Expect(res.Level).To(Equal("mw2"))
	})
})
