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

var _ = Describe("L0 Regex Middleware", func() {
	var (
		ctx  context.Context
		next semanticmiddleware.ClassifyFunc
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Terminal next that records it was called.
		next = func(_ context.Context, _ *semanticmiddleware.ClassifyContext) (semanticmiddleware.ClassifyResult, error) {
			return semanticmiddleware.ClassifyResult{Category: "TERMINAL", Level: "next"}, nil
		}
	})

	Describe("with default config", func() {
		var mw semanticmiddleware.Middleware

		BeforeEach(func() {
			mw = semanticmiddleware.NewL0Regex(semanticmiddleware.L0RegexConfig{})
		})

		DescribeTable("should match follow-up patterns",
			func(input string) {
				cc := &semanticmiddleware.ClassifyContext{Question: input}
				res, err := mw(ctx, cc, next)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.Category).To(Equal("COMPLEX"))
				Expect(res.BypassedLLM).To(BeTrue())
				Expect(res.Level).To(Equal("L0"))
				Expect(cc.IsFollowUp).To(BeTrue())
			},
			Entry("try again", "try again"),
			Entry("pls try again", "pls try again"),
			Entry("please retry", "please retry"),
			Entry("retry again", "retry again"),
			Entry("do it again", "do it again"),
			Entry("but I wanted", "but I wanted something else"),
			Entry("that's not what I", "that's not what I asked"),
			Entry("no, I meant", "no, I meant the other one"),
			Entry("run that again", "run that again"),
			Entry("same thing", "same thing"),
			Entry("you already have", "you already have access to that"),
		)

		DescribeTable("should NOT match non-follow-up messages",
			func(input string) {
				cc := &semanticmiddleware.ClassifyContext{Question: input}
				res, err := mw(ctx, cc, next)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.Category).To(Equal("TERMINAL"))
				Expect(res.Level).To(Equal("next"))
				Expect(cc.IsFollowUp).To(BeFalse())
			},
			Entry("hello", "hello"),
			Entry("check cluster health", "check cluster health"),
			Entry("deploy the app", "deploy the app"),
			Entry("what's the weather", "what's the weather"),
			Entry("list pods in production", "list pods in production"),
		)
	})

	Describe("with Disabled=true", func() {
		It("should pass through to next", func() {
			mw := semanticmiddleware.NewL0Regex(semanticmiddleware.L0RegexConfig{Disabled: true})
			cc := &semanticmiddleware.ClassifyContext{Question: "try again"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("TERMINAL"))
		})
	})

	Describe("with custom ExtraPatterns", func() {
		It("should match user-defined patterns", func() {
			mw := semanticmiddleware.NewL0Regex(semanticmiddleware.L0RegexConfig{
				ExtraPatterns: []string{`(?i)^status\s+check`},
			})
			cc := &semanticmiddleware.ClassifyContext{Question: "status check please"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Category).To(Equal("COMPLEX"))
			Expect(res.BypassedLLM).To(BeTrue())
		})

		It("should ignore invalid regex patterns gracefully", func() {
			mw := semanticmiddleware.NewL0Regex(semanticmiddleware.L0RegexConfig{
				ExtraPatterns: []string{`[invalid`},
			})
			cc := &semanticmiddleware.ClassifyContext{Question: "hello"}
			res, err := mw(ctx, cc, next)
			Expect(err).NotTo(HaveOccurred())
			// Should still work with just default patterns.
			Expect(res.Category).To(Equal("TERMINAL"))
		})
	})
})
