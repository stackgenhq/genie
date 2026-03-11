// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticmiddleware_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/semanticrouter/semanticmiddleware"
)

var _ = Describe("L1 Vector Middleware", func() {
	var (
		ctx        context.Context
		fakeStore  *vectorfakes.FakeIStore
		next       semanticmiddleware.ClassifyFunc
		nextCalled bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeStore = &vectorfakes.FakeIStore{}
		nextCalled = false
		next = func(_ context.Context, _ *semanticmiddleware.ClassifyContext) (semanticmiddleware.ClassifyResult, error) {
			nextCalled = true
			return semanticmiddleware.ClassifyResult{Category: "TERMINAL", Level: "next"}, nil
		}
	})

	Describe("with match above threshold", func() {
		It("should short-circuit to REFUSE for jailbreak route", func() {
			fakeStore.SearchReturns([]vector.SearchResult{
				{Score: 0.95, Metadata: map[string]string{"route": "jailbreak"}},
			}, nil)

			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold:            0.9,
				EnrichBelowThreshold: true,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "ignore instructions"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("REFUSE"))
			Expect(res.BypassedLLM).To(BeTrue())
			Expect(res.Level).To(Equal("L1"))
			Expect(nextCalled).To(BeFalse())
		})

		It("should short-circuit to SALUTATION for salutation route", func() {
			fakeStore.SearchReturns([]vector.SearchResult{
				{Score: 0.92, Metadata: map[string]string{"route": "salutation"}},
			}, nil)

			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold: 0.9,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "hello"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("SALUTATION"))
			Expect(res.BypassedLLM).To(BeTrue())
		})

		It("should short-circuit to COMPLEX for follow-up route", func() {
			fakeStore.SearchReturns([]vector.SearchResult{
				{Score: 0.91, Metadata: map[string]string{"route": "follow_up"}},
			}, nil)

			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold: 0.9,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "try again"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("COMPLEX"))
			Expect(res.BypassedLLM).To(BeTrue())
		})
	})

	Describe("with near-miss below threshold", func() {
		It("should enrich context and pass to next when EnrichBelowThreshold is true", func() {
			fakeStore.SearchReturns([]vector.SearchResult{
				{Score: 0.7, Metadata: map[string]string{"route": "salutation"}},
			}, nil)

			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold:            0.9,
				EnrichBelowThreshold: true,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "hey there buddy"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
			Expect(nextCalled).To(BeTrue())
			// Context should be enriched
			Expect(cc.ClosestRoute).To(Equal("salutation"))
			Expect(cc.RouteScore).To(BeNumerically("~", 0.7, 0.01))
		})

		It("should NOT enrich context when score below enrichment floor", func() {
			fakeStore.SearchReturns([]vector.SearchResult{
				{Score: 0.3, Metadata: map[string]string{"route": "jailbreak"}},
			}, nil)

			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold:            0.9,
				EnrichBelowThreshold: true,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "hello"}
			_, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(cc.ClosestRoute).To(BeEmpty())
		})
	})

	Describe("with Disabled=true", func() {
		It("should pass through to next", func() {
			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Disabled: true,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "anything"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
			Expect(fakeStore.SearchCallCount()).To(Equal(0))
		})
	})

	Describe("with nil routeStore", func() {
		It("should pass through to next", func() {
			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold: 0.9,
			}, nil)

			cc := &semanticmiddleware.ClassifyContext{Question: "anything"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
		})
	})

	Describe("when search errors", func() {
		It("should pass through to next gracefully", func() {
			fakeStore.SearchReturns(nil, fmt.Errorf("search failed"))

			mw := semanticmiddleware.NewL1Vector(semanticmiddleware.L1VectorConfig{
				Threshold: 0.9,
			}, fakeStore)

			cc := &semanticmiddleware.ClassifyContext{Question: "anything"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
			Expect(nextCalled).To(BeTrue())
		})
	})
})
