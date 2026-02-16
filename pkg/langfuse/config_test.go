package langfuse

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {
	DescribeTable("langfuseHost (full URL for HTTP API)",
		func(input, expected string) {
			c := Config{Host: input}
			Expect(c.langfuseHost()).To(Equal(expected))
		},
		Entry("bare hostname gets https://", "langfuse.cloud.stackgen.com", "https://langfuse.cloud.stackgen.com"),
		Entry("https:// is preserved", "https://langfuse.cloud.stackgen.com", "https://langfuse.cloud.stackgen.com"),
		Entry("http:// is preserved", "http://localhost:3000", "http://localhost:3000"),
	)

	DescribeTable("langfuseOTLPEndpoint (hostname:port for OTLP)",
		func(input, expected string) {
			c := Config{Host: input}
			Expect(c.langfuseOTLPEndpoint()).To(Equal(expected))
		},
		Entry("bare hostname gets :443", "langfuse.cloud.stackgen.com", "langfuse.cloud.stackgen.com:443"),
		Entry("hostname with port is unchanged", "langfuse.cloud.stackgen.com:3000", "langfuse.cloud.stackgen.com:3000"),
		Entry("https:// scheme is stripped", "https://langfuse.cloud.stackgen.com", "langfuse.cloud.stackgen.com:443"),
		Entry("http:// scheme is stripped", "http://localhost", "localhost:443"),
		Entry("http:// with port", "http://localhost:3000", "localhost:3000"),
		Entry("https:// with port", "https://langfuse.example.com:8443", "langfuse.example.com:8443"),
	)
})
