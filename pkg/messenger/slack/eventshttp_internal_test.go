package slack

import (
	"bytes"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Events HTTP Internal", func() {
	Describe("urlDecode", func() {
		It("decodes valid url", func() {
			res, err := urlDecode("a+b%20c")
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Equal("a b c"))
		})
		It("fails on incomplete encoding", func() {
			_, err := urlDecode("a%")
			Expect(err).To(HaveOccurred())
		})
		It("fails on invalid hex", func() {
			_, err := urlDecode("a%2z")
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("unhex", func() {
		It("returns correctly", func() {
			val, ok := unhex('0')
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal(byte(0)))

			val, ok = unhex('a')
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal(byte(10)))

			val, ok = unhex('F')
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal(byte(15)))

			_, ok = unhex('z')
			Expect(ok).To(BeFalse())
		})
	})
	Describe("verifySignature", func() {
		It("returns true for valid signature", func() {
			h := &eventsHTTPHandler{signingSecret: "8f742231b10e8888abcd99yyyzzz85a5"}
			req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("foobar")))
			req.Header.Set("X-Slack-Request-Timestamp", "1531420618")
			req.Header.Set("X-Slack-Signature", "v0=0f634490448c74fb103758753b814d0857d227fe078460d71076ea744b8013d6")

			Expect(h.verifySignature(req, []byte("foobar"))).To(BeTrue())
		})
		It("returns false on missing headers", func() {
			h := &eventsHTTPHandler{signingSecret: "secret"}
			req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("foobar")))
			Expect(h.verifySignature(req, []byte("foobar"))).To(BeFalse())
		})
	})
})
