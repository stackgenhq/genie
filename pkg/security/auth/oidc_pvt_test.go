package auth

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
