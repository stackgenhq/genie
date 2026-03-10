// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package htmlutils provides utilities for extracting readable text content
// from raw HTML. It uses golang.org/x/net/html for robust parsing that
// gracefully handles malformed, truncated, and noisy HTML from the wild web.
package htmlutils

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Whitespace-collapsing regexes.
var (
	reBlankLine    = regexp.MustCompile(`(?m)^[\t ]+$`)
	reMultiNewline = regexp.MustCompile(`\n{3,}`)
	reMultiSpace   = regexp.MustCompile(`[\t ]{2,}`)
)

// skipElements is the set of HTML elements whose entire subtree is skipped
// during text extraction. These contain no user-facing prose and are
// typically the source of massive noise (200KB+ JSON blobs in <script>,
// repeated nav/footer boilerplate, inline SVGs, etc.).
var skipElements = map[atom.Atom]bool{
	atom.Script:   true, // JS code + serialised JSON (Next.js __NEXT_DATA__, etc.)
	atom.Style:    true, // CSS rules
	atom.Svg:      true, // inline vector graphics
	atom.Nav:      true, // site-wide navigation links
	atom.Footer:   true, // page/site footers
	atom.Noscript: true, // fallback content for no-JS
	atom.Iframe:   true, // embedded frames
	atom.Object:   true, // embedded objects
	atom.Template: true, // HTML templates (not rendered)
	// NOTE: <header> is intentionally NOT skipped. Many modern sites wrap
	// their main page heading and hero content in <header> tags.
	// Navigation boilerplate is already caught by <nav>.
}

// blockElements emit a newline before/after for readability.
var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Li: true, atom.Tr: true, atom.Blockquote: true, atom.Pre: true,
}

// ExtractText parses raw HTML and returns only the readable text content.
//
// The parser (golang.org/x/net/html) naturally handles all edge cases:
//   - Truncated/unclosed tags (e.g., a 200KB __NEXT_DATA__ <script> cut off
//     at the 512KB response cap — the parser treats it as a single element)
//   - Embedded JSON blobs (safely contained inside <script> elements)
//   - Malformed/nested HTML
//
// This typically reduces 500KB of raw HTML to ~5–15KB of clean text.
func ExtractText(rawHTML string) string {
	// Quick check: if it doesn't look like HTML, return as-is.
	peek := rawHTML
	if len(peek) > 1000 {
		peek = peek[:1000]
	}
	if !strings.Contains(peek, "<") {
		return rawHTML
	}

	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		// Parsing failure is extremely rare — fall back to basic cleanup.
		return CollapseWhitespace(rawHTML)
	}

	var sb strings.Builder
	sb.Grow(len(rawHTML) / 10) // text is typically ~10% of raw HTML
	walkTree(doc, &sb)

	return CollapseWhitespace(sb.String())
}

// walkTree recursively walks the HTML node tree, collecting text content
// and skipping noise elements.
func walkTree(n *html.Node, sb *strings.Builder) {
	if n.Type == html.ElementNode && skipElements[n.DataAtom] {
		return // skip entire subtree
	}

	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}

	// Line break for <br> and block-level elements.
	if n.Type == html.ElementNode {
		if n.DataAtom == atom.Br {
			sb.WriteByte('\n')
		} else if blockElements[n.DataAtom] {
			sb.WriteByte('\n')
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkTree(c, sb)
	}

	// Trailing newline after block elements for separation.
	if n.Type == html.ElementNode && blockElements[n.DataAtom] {
		sb.WriteByte('\n')
	}
}

// CollapseWhitespace normalises whitespace: removes blank lines, collapses
// runs of spaces, and limits consecutive newlines to at most two.
func CollapseWhitespace(s string) string {
	s = strings.NewReplacer("\u00a0", " ", "\u2028", "\n", "\u2029", "\n", "\u200b", "").Replace(s)
	s = reBlankLine.ReplaceAllString(s, "")
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = reMultiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
