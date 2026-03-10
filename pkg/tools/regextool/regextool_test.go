// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package regextool

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Regex Tool (assist_with_regular_expressions)", func() {
	var r *regexTools

	BeforeEach(func() {
		r = newRegexTools()
	})

	Describe("match", func() {
		DescribeTable("tests pattern matching",
			func(pattern, input string, expected string) {
				resp, err := r.regex(context.Background(), regexRequest{
					Operation: "match", Pattern: pattern, Input: input,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Result).To(Equal(expected))
			},
			Entry("matches email", `[\w.]+@[\w.]+`, "user@example.com", "true"),
			Entry("no match", `^\d+$`, "abc", "false"),
			Entry("partial match", `\d+`, "abc123def", "true"),
		)
	})

	Describe("find_all", func() {
		It("finds all matches", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation: "find_all",
				Pattern:   `\d+`,
				Input:     "foo12bar34baz56",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(3))
			Expect(resp.Result).To(ContainSubstring("12"))
			Expect(resp.Result).To(ContainSubstring("34"))
			Expect(resp.Result).To(ContainSubstring("56"))
		})

		It("returns empty array for no matches", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation: "find_all",
				Pattern:   `\d+`,
				Input:     "no numbers here",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(0))
		})
	})

	Describe("replace", func() {
		It("replaces matches with replacement string", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation:   "replace",
				Pattern:     `\d+`,
				Input:       "foo123bar456",
				Replacement: "NUM",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("fooNUMbarNUM"))
		})

		It("supports group references in replacement", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation:   "replace",
				Pattern:     `(\w+)@(\w+)`,
				Input:       "user@host",
				Replacement: "$2/$1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("host/user"))
		})
	})

	Describe("split", func() {
		It("splits string by pattern", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation: "split",
				Pattern:   `[,;]\s*`,
				Input:     "a, b; c,d",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(4))
			Expect(resp.Result).To(ContainSubstring("a"))
			Expect(resp.Result).To(ContainSubstring("d"))
		})
	})

	Describe("extract_groups", func() {
		It("extracts named capture groups", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation: "extract_groups",
				Pattern:   `(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})`,
				Input:     "today is 2024-01-15 ok",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(3))
			Expect(resp.Result).To(ContainSubstring(`"year":"2024"`))
			Expect(resp.Result).To(ContainSubstring(`"month":"01"`))
			Expect(resp.Result).To(ContainSubstring(`"day":"15"`))
		})

		It("extracts numbered groups when unnamed", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation: "extract_groups",
				Pattern:   `(\w+)@(\w+)\.(\w+)`,
				Input:     "user@example.com",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Count).To(Equal(3))
			Expect(resp.Result).To(ContainSubstring("user"))
			Expect(resp.Result).To(ContainSubstring("example"))
			Expect(resp.Result).To(ContainSubstring("com"))
		})

		It("returns null for no match", func() {
			resp, err := r.regex(context.Background(), regexRequest{
				Operation: "extract_groups",
				Pattern:   `(\d+)`,
				Input:     "no numbers",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("null"))
		})
	})

	It("returns error for invalid regex pattern", func() {
		_, err := r.regex(context.Background(), regexRequest{
			Operation: "match",
			Pattern:   `[invalid`,
			Input:     "test",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid regex"))
	})

	It("returns error for empty pattern", func() {
		_, err := r.regex(context.Background(), regexRequest{
			Operation: "match",
			Input:     "test",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("pattern is required"))
	})

	It("returns error for empty input", func() {
		_, err := r.regex(context.Background(), regexRequest{
			Operation: "match",
			Pattern:   `\d+`,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("input is required"))
	})

	It("returns error for unsupported operation", func() {
		_, err := r.regex(context.Background(), regexRequest{
			Operation: "transform",
			Pattern:   `\d+`,
			Input:     "test",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported operation"))
	})
})

var _ = Describe("Regex ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("assist_with_regular_expressions"))
	})
})
