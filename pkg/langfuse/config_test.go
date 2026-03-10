// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package langfuse

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
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

var _ = Describe("DefaultConfig", func() {
	It("should resolve secrets from SecretProvider", func() {
		sp := &securityfakes.FakeSecretProvider{}
		sp.GetSecretStub = func(_ context.Context, req security.GetSecretRequest) (string, error) {
			secrets := map[string]string{
				"LANGFUSE_PUBLIC_KEY": "pk-test",
				"LANGFUSE_SECRET_KEY": "sk-test",
				"LANGFUSE_HOST":       "langfuse.example.com",
			}
			return secrets[req.Name], nil
		}
		cfg := DefaultConfig(context.Background(), sp)
		Expect(cfg.PublicKey).To(Equal("pk-test"))
		Expect(cfg.SecretKey).To(Equal("sk-test"))
		Expect(cfg.Host).To(Equal("langfuse.example.com"))
	})

	It("should return empty config when secrets are not set", func() {
		sp := &securityfakes.FakeSecretProvider{}
		cfg := DefaultConfig(context.Background(), sp)
		Expect(cfg.PublicKey).To(BeEmpty())
		Expect(cfg.SecretKey).To(BeEmpty())
		Expect(cfg.Host).To(BeEmpty())
	})
})

var _ = Describe("Config.NewClient", func() {
	It("should return noopClient when credentials are missing", func() {
		cfg := Config{PublicKey: "", SecretKey: "", Host: ""}
		c := cfg.NewClient()
		Expect(c).NotTo(BeNil())
		// noopClient should return the default prompt
		result := c.GetPrompt(context.Background(), "my_prompt", "default_value")
		Expect(result).To(Equal("default_value"))
	})

	It("should return noopClient when only public key is set", func() {
		cfg := Config{PublicKey: "pk", SecretKey: "", Host: ""}
		c := cfg.NewClient()
		result := c.GetPrompt(context.Background(), "test", "fallback")
		Expect(result).To(Equal("fallback"))
	})

	It("should return real client when all credentials are set", func() {
		cfg := Config{PublicKey: "pk", SecretKey: "sk", Host: "langfuse.example.com"}
		c := cfg.NewClient()
		Expect(c).NotTo(BeNil())
		// The client is a *client struct, not noopClient
		_, isNoop := c.(*noopClient)
		Expect(isNoop).To(BeFalse())
	})
})

var _ = Describe("GetPrompt (global)", func() {
	BeforeEach(func() {
		// Reset global state
		defaultClient = nil
	})

	It("should return default when no client is configured", func() {
		result := GetPrompt(context.Background(), "my_prompt", "default_text")
		Expect(result).To(Equal("default_text"))
	})

	It("should delegate to defaultClient when configured", func() {
		defaultClient = &noopClient{}
		result := GetPrompt(context.Background(), "my_prompt", "default_text")
		Expect(result).To(Equal("default_text"))
	})
})
