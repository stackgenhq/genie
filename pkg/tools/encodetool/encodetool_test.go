package encodetool

import (
	"context"

	"github.com/stackgenhq/genie/pkg/security"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Encode Tool encode_string", func() {
	var e *encodeTools

	BeforeEach(func() {
		e = newEncodeTools()
	})

	Describe("base64_encode", func() {
		DescribeTable("encodes strings correctly",
			func(input, expected string) {
				resp, err := e.encode(context.Background(), encodeRequest{Operation: "base64_encode", Input: input})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Result).To(Equal(expected))
			},
			Entry("hello world", "hello world", "aGVsbG8gd29ybGQ="),
			Entry("empty-ish", "a", "YQ=="),
			Entry("special chars", "foo:bar@baz", "Zm9vOmJhckBiYXo="),
		)
	})

	Describe("base64_decode", func() {
		It("decodes valid base64", func() {
			resp, err := e.encode(context.Background(), encodeRequest{Operation: "base64_decode", Input: "aGVsbG8gd29ybGQ="})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("hello world"))
		})

		It("returns error for invalid base64", func() {
			_, err := e.encode(context.Background(), encodeRequest{Operation: "base64_decode", Input: "not-valid-base64!!!"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("base64 decode failed"))
		})
	})

	Describe("url_encode", func() {
		DescribeTable("encodes URLs correctly",
			func(input, expected string) {
				resp, err := e.encode(context.Background(), encodeRequest{Operation: "url_encode", Input: input})
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Result).To(Equal(expected))
			},
			Entry("spaces", "hello world", "hello+world"),
			Entry("special chars", "key=value&foo=bar", "key%3Dvalue%26foo%3Dbar"),
		)
	})

	Describe("url_decode", func() {
		It("decodes URL-encoded strings", func() {
			resp, err := e.encode(context.Background(), encodeRequest{Operation: "url_decode", Input: "hello+world"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("hello world"))
		})

		It("returns error for invalid encoding", func() {
			_, err := e.encode(context.Background(), encodeRequest{Operation: "url_decode", Input: "%ZZinvalid"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("sha256", func() {
		It("computes correct SHA-256 hash", func() {
			resp, err := e.encode(context.Background(), encodeRequest{Operation: "sha256", Input: "hello"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"))
		})
	})

	Describe("md5", func() {
		It("rejects md5 with security policy error", func() {
			_, err := e.encode(context.Background(), encodeRequest{Operation: "md5", Input: "hello"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("disabled by security policy"))
			Expect(err.Error()).To(ContainSubstring("sha256"))
		})
	})

	It("returns error for empty input", func() {
		_, err := e.encode(context.Background(), encodeRequest{Operation: "base64_encode", Input: ""})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("input is required"))
	})

	It("returns error for unsupported operation", func() {
		_, err := e.encode(context.Background(), encodeRequest{Operation: "rot13", Input: "hello"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported operation"))
	})
})

var _ = Describe("Encode ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider(security.CryptoConfig{})
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("encode_string"))
	})
})
