package auth

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("sanitizeReturnTo (private)", func() {
	const fallback = "/ui/chat.html"

	DescribeTable("validates and sanitizes redirect paths",
		func(input, expected string) {
			Expect(sanitizeReturnTo(input)).To(Equal(expected))
		},
		// Valid local paths
		Entry("valid root path", "/", "/"),
		Entry("valid local path", "/ui/chat.html", "/ui/chat.html"),
		Entry("valid nested path", "/dashboard/settings", "/dashboard/settings"),
		Entry("valid path with query string", "/page?foo=bar", "/page?foo=bar"),

		// Fallback cases
		Entry("empty string", "", fallback),
		Entry("protocol-relative //", "//evil.com", fallback),
		Entry("backslash in second position /\\", "/\\evil.com", fallback),
		Entry("plain backslash", "\\evil.com", fallback),
		Entry("absolute http URL", "http://evil.com", fallback),
		Entry("absolute https URL", "https://evil.com", fallback),
		Entry("no leading slash", "evil.com", fallback),

		// Encoded attack vectors
		Entry("encoded backslash %5C", "/%5Cevil.com", fallback),
		Entry("double encoded backslash %5C%5C", "/%5C%5Cevil.com", fallback),
		Entry("encoded forward slash %2F", "/%2Fevil.com", fallback),

		// Control characters
		Entry("contains null byte", "/page\x00evil", fallback),
		Entry("contains newline", "/page\nevil", fallback),
		Entry("contains tab", "/page\tevil", fallback),
		Entry("contains DEL character", "/page\x7fevil", fallback),
	)
})

var _ = Describe("isDomainAllowed (private)", func() {
	DescribeTable("matches domain or email against allow list",
		func(val string, allowed []string, expected bool) {
			Expect(isDomainAllowed(val, allowed)).To(Equal(expected))
		},
		Entry("exact domain match", "example.com", []string{"example.com"}, true),
		Entry("case-insensitive domain match", "Example.COM", []string{"example.com"}, true),
		Entry("email suffix match", "user@example.com", []string{"example.com"}, true),
		Entry("email suffix case-insensitive", "user@EXAMPLE.COM", []string{"example.com"}, true),
		Entry("no match", "other.com", []string{"example.com"}, false),
		Entry("empty value", "", []string{"example.com"}, false),
		Entry("empty allow list", "anything@example.com", []string{}, false),
		Entry("nil allow list", "anything@example.com", nil, false),
		Entry("multiple domains first match", "user@a.com", []string{"a.com", "b.com"}, true),
		Entry("multiple domains second match", "user@b.com", []string{"a.com", "b.com"}, true),
		Entry("multiple domains no match", "user@c.com", []string{"a.com", "b.com"}, false),
	)
})
