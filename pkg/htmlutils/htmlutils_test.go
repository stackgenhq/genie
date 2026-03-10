// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package htmlutils

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ExtractText", func() {

	It("extracts text from basic HTML", func() {
		input := `<html><head><title>Test</title></head><body><h1>Hello</h1><p>World</p></body></html>`
		result := ExtractText(input)
		Expect(result).To(ContainSubstring("Hello"))
		Expect(result).To(ContainSubstring("World"))
		Expect(result).To(ContainSubstring("Test"))
	})

	It("strips script tags and their content", func() {
		input := `<html><body><p>Visible</p><script>var x = {"huge":"json","data":"blob"};</script><p>Also visible</p></body></html>`
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("huge"))
		Expect(result).NotTo(ContainSubstring("blob"))
		Expect(result).To(ContainSubstring("Visible"))
		Expect(result).To(ContainSubstring("Also visible"))
	})

	It("strips truncated __NEXT_DATA__ script (simulated 512KB cut)", func() {
		// Simulate a truncated __NEXT_DATA__ script — no closing </script> tag.
		// This is the exact scenario that caused 200KB of CLDR JSON to leak
		// into the summarizer when using regex-based stripping.
		jsonBlob := `{"props":{"pageProps":{"localeData":` + strings.Repeat(`"x",`, 10000) + `"end"}}}`
		input := `<html><body><p>Content</p><script id="__NEXT_DATA__" type="application/json">` + jsonBlob
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("localeData"))
		Expect(result).NotTo(ContainSubstring("pageProps"))
		Expect(result).To(ContainSubstring("Content"))
	})

	It("strips style tags", func() {
		input := `<html><body><style>.foo { color: red; }</style><p>Visible</p></body></html>`
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("color"))
		Expect(result).To(ContainSubstring("Visible"))
	})

	It("strips SVG elements", func() {
		input := `<html><body><svg><path d="M0 0"/><text>SVG text</text></svg><p>Visible</p></body></html>`
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("SVG text"))
		Expect(result).To(ContainSubstring("Visible"))
	})

	It("strips nav and footer but preserves header content", func() {
		input := `<html><body>
			<nav><a href="/">Home</a><a href="/about">About</a></nav>
			<header><h1>Site Title</h1></header>
			<main><p>Main content here</p></main>
			<footer><p>Copyright 2024</p></footer>
		</body></html>`
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("Home"))
		Expect(result).NotTo(ContainSubstring("About"))
		// Header content is intentionally preserved — many sites put
		// real page headings inside <header> tags.
		Expect(result).To(ContainSubstring("Site Title"))
		Expect(result).NotTo(ContainSubstring("Copyright"))
		Expect(result).To(ContainSubstring("Main content here"))
	})

	It("strips noscript and iframe elements", func() {
		input := `<html><body><noscript>Enable JS</noscript><iframe src="ad.html">Ad</iframe><p>Real</p></body></html>`
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("Enable JS"))
		Expect(result).To(ContainSubstring("Real"))
	})

	It("passes through plain text unchanged", func() {
		input := "This is just plain text with no HTML tags at all."
		result := ExtractText(input)
		Expect(result).To(Equal(input))
	})

	It("returns empty for empty input", func() {
		Expect(ExtractText("")).To(BeEmpty())
	})

	It("handles multiple script tags including large inline JSON", func() {
		input := `<html><body>
			<script src="app.js"></script>
			<script>console.log("init")</script>
			<script type="application/json">{"key":"value","nested":{"deep":"data"}}</script>
			<p>Actual content</p>
		</body></html>`
		result := ExtractText(input)
		Expect(result).NotTo(ContainSubstring("console"))
		Expect(result).NotTo(ContainSubstring("nested"))
		Expect(result).To(ContainSubstring("Actual content"))
	})
})

var _ = Describe("CollapseWhitespace", func() {

	It("collapses multiple spaces into one", func() {
		result := CollapseWhitespace("hello   world")
		Expect(result).To(Equal("hello world"))
	})

	It("limits consecutive newlines to two", func() {
		result := CollapseWhitespace("line1\n\n\n\n\nline2")
		Expect(result).To(Equal("line1\n\nline2"))
	})

	It("removes whitespace-only lines", func() {
		result := CollapseWhitespace("hello\n   \nworld")
		Expect(result).To(Equal("hello\n\nworld"))
	})

	It("trims leading and trailing whitespace", func() {
		result := CollapseWhitespace("  hello world  ")
		Expect(result).To(Equal("hello world"))
	})
})
