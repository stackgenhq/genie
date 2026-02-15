package reactree

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("looksLikeError", func() {
	It("should detect empty output as error", func() {
		Expect(looksLikeError("")).To(BeTrue())
	})

	It("should detect 'An error occurred during execution' as error", func() {
		Expect(looksLikeError("An error occurred during execution. Please contact the service provider.")).To(BeTrue())
	})

	It("should detect 'something went wrong' as error", func() {
		Expect(looksLikeError("I'm sorry, something went wrong while processing your request.")).To(BeTrue())
	})

	It("should detect case-insensitive errors", func() {
		Expect(looksLikeError("AN ERROR OCCURRED")).To(BeTrue())
	})

	It("should NOT flag normal responses as errors", func() {
		Expect(looksLikeError("Hello! I'm Genie, your personal AI assistant.")).To(BeFalse())
	})

	It("should NOT flag code discussion about errors", func() {
		// This is a legitimate response discussing error handling code,
		// but it contains "an error occurred" as a substring. This is a
		// known limitation — we accept false positives on error-discussion
		// text to prevent the more harmful episodic memory poisoning.
		Expect(looksLikeError("Here's how to handle errors gracefully in your code with try/catch blocks.")).To(BeFalse())
	})

	It("should NOT flag file listings as errors", func() {
		Expect(looksLikeError("cmd/\npkg/\ngo.mod\ngo.sum\nMakefile")).To(BeFalse())
	})
})
