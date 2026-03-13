// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource/docparser"
)

var _ = Describe("MIME Detection", func() {
	DescribeTable("detectMIME",
		func(filename, expected string) {
			Expect(docparser.DetectMIME(filename)).To(Equal(expected))
		},
		Entry("PDF", "report.pdf", "application/pdf"),
		Entry("DOCX", "doc.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"),
		Entry("PPTX", "slides.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"),
		Entry("XLSX", "data.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"),
		Entry("Markdown", "README.md", "text/markdown"),
		Entry("Unknown", "mystery", "application/octet-stream"),
		Entry("Case insensitive", "REPORT.PDF", "application/pdf"),
	)
})

var _ = Describe("splitOnPageMarkers", func() {
	It("returns whole text when no page markers", func() {
		text := "Hello world\nSecond line"
		pages := docparser.SplitOnPageMarkers(text)
		Expect(pages).To(HaveLen(1))
		Expect(pages[0]).To(ContainSubstring("Hello world"))
		Expect(pages[0]).To(ContainSubstring("Second line"))
	})

	It("splits on page markers", func() {
		text := "Page one content\n--- PAGE 1 ---\nPage two content\n--- PAGE 2 ---\nPage three"
		pages := docparser.SplitOnPageMarkers(text)
		Expect(pages).To(HaveLen(3))
		Expect(pages[0]).To(ContainSubstring("Page one"))
		Expect(pages[1]).To(ContainSubstring("Page two"))
		Expect(pages[2]).To(ContainSubstring("Page three"))
	})

	It("handles case-insensitive markers", func() {
		text := "--- page 1 ---\nContent"
		pages := docparser.SplitOnPageMarkers(text)
		Expect(pages).To(HaveLen(1))
		Expect(pages[0]).To(ContainSubstring("Content"))
	})
})
