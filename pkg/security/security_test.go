// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package security_test

import (
	"context"
	"crypto/tls"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/security"
)

var _ = Describe("SecretProvider", func() {

	Describe("envProvider", func() {
		It("should return the value of a set environment variable", func(ctx context.Context) {
			sp := security.NewEnvProvider()
			os.Setenv("TEST_SECRET_KEY", "test-secret-value")
			defer os.Unsetenv("TEST_SECRET_KEY")

			val, err := sp.GetSecret(ctx, security.GetSecretRequest{Name: "TEST_SECRET_KEY"})
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal("test-secret-value"))
		})

		It("should return empty string for unset variables", func(ctx context.Context) {
			sp := security.NewEnvProvider()
			os.Unsetenv("NONEXISTENT_SECRET_KEY")

			val, err := sp.GetSecret(ctx, security.GetSecretRequest{Name: "NONEXISTENT_SECRET_KEY"})
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(BeEmpty())
		})

		It("should invoke WithSecretLookupAuditEnv only when a non-empty value is returned", func(ctx context.Context) {
			var lookedUp []string
			sp := security.NewEnvProvider(security.WithSecretLookupAuditEnv(func(_ context.Context, req security.GetSecretRequest) {
				lookedUp = append(lookedUp, req.Name)
			}))

			os.Setenv("AUDIT_SECRET_SET", "v1")
			defer os.Unsetenv("AUDIT_SECRET_SET")
			os.Unsetenv("AUDIT_SECRET_UNSET")

			_, _ = sp.GetSecret(ctx, security.GetSecretRequest{Name: "AUDIT_SECRET_SET"})
			_, _ = sp.GetSecret(ctx, security.GetSecretRequest{Name: "AUDIT_SECRET_UNSET"})
			_, _ = sp.GetSecret(ctx, security.GetSecretRequest{Name: "AUDIT_SECRET_SET"})

			Expect(lookedUp).To(Equal([]string{"AUDIT_SECRET_SET", "AUDIT_SECRET_SET"}))
		})
	})

	Describe("Manager", func() {

		Context("with constantvar backend", func() {
			It("should resolve a secret from runtimevar URL", func(ctx context.Context) {
				cfg := security.Config{
					Secrets: map[string]string{
						"MY_SECRET": "constant://?val=hello-from-runtimevar&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "MY_SECRET"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("hello-from-runtimevar"))
			})
		})

		Context("with env fallback", func() {
			It("should fall back to os.Getenv when no URL mapping exists", func(ctx context.Context) {
				os.Setenv("FALLBACK_SECRET", "from-env")
				defer os.Unsetenv("FALLBACK_SECRET")

				cfg := security.Config{
					Secrets: map[string]string{
						"OTHER_SECRET": "constant://?val=other&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "FALLBACK_SECRET"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("from-env"))
			})

			It("should return empty string for unmapped and unset secrets", func(ctx context.Context) {
				os.Unsetenv("TOTALLY_MISSING")

				mgr := security.NewManager(ctx, security.Config{})
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "TOTALLY_MISSING"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(BeEmpty())
			})
		})

		Context("with invalid URL", func() {
			It("should return an error for an unsupported scheme", func(ctx context.Context) {
				cfg := security.Config{
					Secrets: map[string]string{
						"BAD_SECRET": "nosuchscheme://foo?decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				_, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "BAD_SECRET"})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("BAD_SECRET"))
			})
		})

		Context("WithSecretLookupAudit", func() {
			It("should invoke callback when GetSecret succeeds from runtimevar", func(ctx context.Context) {
				var lookedUp []string
				cfg := security.Config{
					Secrets: map[string]string{
						"AUDIT_SECRET": "constant://?val=secret-val&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg, security.WithSecretLookupAudit(func(_ context.Context, req security.GetSecretRequest) {
					lookedUp = append(lookedUp, req.Name)
				}))
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "AUDIT_SECRET"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("secret-val"))
				Expect(lookedUp).To(Equal([]string{"AUDIT_SECRET"}))
			})

			It("should invoke callback when GetSecret succeeds from env fallback", func(ctx context.Context) {
				os.Setenv("ENV_AUDIT_SECRET", "from-env")
				defer os.Unsetenv("ENV_AUDIT_SECRET")

				var lookedUp []string
				cfg := security.Config{
					Secrets: map[string]string{
						"OTHER": "constant://?val=other&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg, security.WithSecretLookupAudit(func(_ context.Context, req security.GetSecretRequest) {
					lookedUp = append(lookedUp, req.Name)
				}))
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "ENV_AUDIT_SECRET"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("from-env"))
				Expect(lookedUp).To(Equal([]string{"ENV_AUDIT_SECRET"}))
			})

			It("should not invoke callback when GetSecret returns an error", func(ctx context.Context) {
				var lookedUp []string
				cfg := security.Config{
					Secrets: map[string]string{
						"BAD": "nosuchscheme://x?decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg, security.WithSecretLookupAudit(func(_ context.Context, req security.GetSecretRequest) {
					lookedUp = append(lookedUp, req.Name)
				}))
				defer mgr.Close()

				_, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "BAD"})
				Expect(err).To(HaveOccurred())
				Expect(lookedUp).To(BeEmpty())
			})
		})

		Describe("Close", func() {
			It("should close all opened variables without error", func(ctx context.Context) {
				cfg := security.Config{
					Secrets: map[string]string{
						"S1": "constant://?val=v1&decoder=string",
						"S2": "constant://?val=v2&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg)

				_, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "S1"})
				Expect(err).ToNot(HaveOccurred())
				_, err = mgr.GetSecret(ctx, security.GetSecretRequest{Name: "S2"})
				Expect(err).ToNot(HaveOccurred())

				Expect(mgr.Close()).To(Succeed())
			})

			It("should be safe to call multiple times", func(ctx context.Context) {
				mgr := security.NewManager(ctx, security.Config{})
				Expect(mgr.Close()).To(Succeed())
				Expect(mgr.Close()).To(Succeed())
			})

			It("should still resolve secrets after Close via on-demand retriever", func(ctx context.Context) {
				cfg := security.Config{
					Secrets: map[string]string{
						"S1": "constant://?val=v1&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg)

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "S1"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("v1"))

				Expect(mgr.Close()).To(Succeed())

				val, err = mgr.GetSecret(ctx, security.GetSecretRequest{Name: "S1"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("v1"))

				Expect(mgr.Close()).To(Succeed())
			})

			It("should still allow env fallback after Close", func(ctx context.Context) {
				os.Setenv("AFTER_CLOSE_SECRET", "still-works")
				defer os.Unsetenv("AFTER_CLOSE_SECRET")

				mgr := security.NewManager(ctx, security.Config{})
				Expect(mgr.Close()).To(Succeed())

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "AFTER_CLOSE_SECRET"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("still-works"))
			})
		})

		Context("with path in runtimevar URL (gjson extraction)", func() {
			It("should extract a top-level field from a JSON secret", func(ctx context.Context) {
				jsonBlob := `{"api_key":"sk-abc123","region":"us-east-1"}`
				cfg := security.Config{
					Secrets: map[string]string{
						"CLOUD_CREDS": "constant://?val=" + jsonBlob + "&path=api_key",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "CLOUD_CREDS"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("sk-abc123"))
			})

			It("should extract a nested field using dot notation", func(ctx context.Context) {
				jsonBlob := `{"database":{"host":"db.example.com","port":5432}}`
				cfg := security.Config{
					Secrets: map[string]string{
						"DB_HOST": "constant://?val=" + jsonBlob + "&path=database.host",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "DB_HOST"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("db.example.com"))
			})

			It("should extract a numeric value as string", func(ctx context.Context) {
				jsonBlob := `{"database":{"host":"db.example.com","port":5432}}`
				cfg := security.Config{
					Secrets: map[string]string{
						"DB_PORT": "constant://?val=" + jsonBlob + "&path=database.port",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "DB_PORT"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("5432"))
			})

			It("should extract an array element", func(ctx context.Context) {
				jsonBlob := `{"hosts":["h1.example.com","h2.example.com"]}`
				cfg := security.Config{
					Secrets: map[string]string{
						"FIRST_HOST": "constant://?val=" + jsonBlob + "&path=hosts.0",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "FIRST_HOST"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("h1.example.com"))
			})

			It("should return an error when the URL path does not match anything", func(ctx context.Context) {
				jsonBlob := `{"api_key":"sk-abc123"}`
				cfg := security.Config{
					Secrets: map[string]string{
						"SIMPLE_SECRET": "constant://?val=" + jsonBlob + "&path=nonexistent.field",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				_, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "SIMPLE_SECRET"})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("path"))
				Expect(err.Error()).To(ContainSubstring("nonexistent.field"))
			})

			It("should return the full value when no path parameter is present", func(ctx context.Context) {
				cfg := security.Config{
					Secrets: map[string]string{
						"PLAIN_SECRET": "constant://?val=just-a-string&decoder=string",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "PLAIN_SECRET"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("just-a-string"))
			})

			It("should handle a deeply nested path", func(ctx context.Context) {
				jsonBlob := `{"a":{"b":{"c":{"d":"deep-value"}}}}`
				cfg := security.Config{
					Secrets: map[string]string{
						"NESTED": "constant://?val=" + jsonBlob + "&path=a.b.c.d",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "NESTED"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("deep-value"))
			})

			It("should allow multiple secrets to share the same runtimevar URL with different paths", func(ctx context.Context) {
				jsonBlob := `{"gmail_pw":"secret1","gh_token":"secret2","linear_key":"secret3"}`
				cfg := security.Config{
					Secrets: map[string]string{
						"EMAIL_PASSWORD": "constant://?val=" + jsonBlob + "&path=gmail_pw",
						"GITHUB_TOKEN":   "constant://?val=" + jsonBlob + "&path=gh_token",
						"LINEAR_API_KEY": "constant://?val=" + jsonBlob + "&path=linear_key",
					},
				}
				mgr := security.NewManager(ctx, cfg)
				defer mgr.Close()

				v1, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "EMAIL_PASSWORD"})
				Expect(err).ToNot(HaveOccurred())
				Expect(v1).To(Equal("secret1"))

				v2, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "GITHUB_TOKEN"})
				Expect(err).ToNot(HaveOccurred())
				Expect(v2).To(Equal("secret2"))

				v3, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "LINEAR_API_KEY"})
				Expect(err).ToNot(HaveOccurred())
				Expect(v3).To(Equal("secret3"))
			})
		})

		Context("with path in secret name for env fallback", func() {
			It("should extract a field from a JSON env var", func(ctx context.Context) {
				jsonBlob := `{"token":"env-token-value","scope":"read"}`
				os.Setenv("JSON_ENV_SECRET", jsonBlob)
				defer os.Unsetenv("JSON_ENV_SECRET")

				mgr := security.NewManager(ctx, security.Config{})
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "JSON_ENV_SECRET?path=token"})
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("env-token-value"))
			})

			It("should return an error when env value is not valid JSON and path is requested", func(ctx context.Context) {
				os.Setenv("PLAIN_ENV_SECRET", "not-json")
				defer os.Unsetenv("PLAIN_ENV_SECRET")

				mgr := security.NewManager(ctx, security.Config{})
				defer mgr.Close()

				_, err := mgr.GetSecret(ctx, security.GetSecretRequest{Name: "PLAIN_ENV_SECRET?path=some.field"})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("path"))
			})
		})
	})
})

var _ = Describe("CryptoConfig", func() {
	Describe("TLSConfig", func() {
		It("returns a config with minimum TLS 1.2", func() {
			cfg := security.DefaultCryptoConfig()
			tlsCfg := cfg.TLSConfig()
			Expect(tlsCfg).NotTo(BeNil())
			Expect(tlsCfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		})

		It("returns only TLS 1.2 ECDHE cipher suites", func() {
			cfg := security.DefaultCryptoConfig()
			tlsCfg := cfg.TLSConfig()
			Expect(tlsCfg).NotTo(BeNil())
			Expect(tlsCfg.CipherSuites).NotTo(BeEmpty())
			// CipherSuites in Go only applies to TLS 1.0–1.2; all should be ECDHE (forward secrecy).
			allowed := map[uint16]bool{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:       true,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:       true,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:         true,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:         true,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256:   true,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256: true,
			}
			for _, id := range tlsCfg.CipherSuites {
				Expect(allowed[id]).To(BeTrue(), "cipher suite %d should be in allowed TLS 1.2 list", id)
			}
		})
	})
})
