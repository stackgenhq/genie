package security_test

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/security"
)

var _ = Describe("SecretProvider", func() {

	Describe("envProvider", func() {
		var (
			ctx context.Context
			sp  security.SecretProvider
		)

		BeforeEach(func() {
			ctx = context.Background()
			sp = security.NewEnvProvider()
		})

		It("should return the value of a set environment variable", func() {
			os.Setenv("TEST_SECRET_KEY", "test-secret-value")
			defer os.Unsetenv("TEST_SECRET_KEY")

			val, err := sp.GetSecret(ctx, "TEST_SECRET_KEY")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal("test-secret-value"))
		})

		It("should return empty string for unset variables", func() {
			os.Unsetenv("NONEXISTENT_SECRET_KEY")

			val, err := sp.GetSecret(ctx, "NONEXISTENT_SECRET_KEY")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(BeEmpty())
		})
	})

	Describe("Manager", func() {
		var (
			ctx context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		Context("with constantvar backend", func() {
			It("should resolve a secret from runtimevar URL", func() {
				cfg := security.Config{
					Secrets: map[string]string{
						"MY_SECRET": "constant://?val=hello-from-runtimevar&decoder=string",
					},
				}
				mgr := security.NewManager(cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, "MY_SECRET")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("hello-from-runtimevar"))
			})

			It("should cache the variable and return consistent values", func() {
				cfg := security.Config{
					Secrets: map[string]string{
						"CACHED_SECRET": "constant://?val=cached-value&decoder=string",
					},
				}
				mgr := security.NewManager(cfg)
				defer mgr.Close()

				val1, err := mgr.GetSecret(ctx, "CACHED_SECRET")
				Expect(err).ToNot(HaveOccurred())

				val2, err := mgr.GetSecret(ctx, "CACHED_SECRET")
				Expect(err).ToNot(HaveOccurred())

				Expect(val1).To(Equal(val2))
				Expect(val1).To(Equal("cached-value"))
			})
		})

		Context("with env fallback", func() {
			It("should fall back to os.Getenv when no URL mapping exists", func() {
				os.Setenv("FALLBACK_SECRET", "from-env")
				defer os.Unsetenv("FALLBACK_SECRET")

				cfg := security.Config{
					Secrets: map[string]string{
						"OTHER_SECRET": "constant://?val=other&decoder=string",
					},
				}
				mgr := security.NewManager(cfg)
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, "FALLBACK_SECRET")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("from-env"))
			})

			It("should return empty string for unmapped and unset secrets", func() {
				os.Unsetenv("TOTALLY_MISSING")

				mgr := security.NewManager(security.Config{})
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, "TOTALLY_MISSING")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(BeEmpty())
			})
		})

		Context("with invalid URL", func() {
			It("should return an error for an unsupported scheme", func() {
				cfg := security.Config{
					Secrets: map[string]string{
						"BAD_SECRET": "nosuchscheme://foo?decoder=string",
					},
				}
				mgr := security.NewManager(cfg)
				defer mgr.Close()

				_, err := mgr.GetSecret(ctx, "BAD_SECRET")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("BAD_SECRET"))
			})
		})

		Describe("Close", func() {
			It("should close all opened variables without error", func() {
				cfg := security.Config{
					Secrets: map[string]string{
						"S1": "constant://?val=v1&decoder=string",
						"S2": "constant://?val=v2&decoder=string",
					},
				}
				mgr := security.NewManager(cfg)

				// Open both variables
				_, err := mgr.GetSecret(ctx, "S1")
				Expect(err).ToNot(HaveOccurred())
				_, err = mgr.GetSecret(ctx, "S2")
				Expect(err).ToNot(HaveOccurred())

				// Close should succeed
				Expect(mgr.Close()).To(Succeed())
			})

			It("should be safe to call multiple times", func() {
				mgr := security.NewManager(security.Config{})
				Expect(mgr.Close()).To(Succeed())
				Expect(mgr.Close()).To(Succeed())
			})

			It("should evict cache on Close and re-open on next GetSecret", func() {
				cfg := security.Config{
					Secrets: map[string]string{
						"S1": "constant://?val=v1&decoder=string",
					},
				}
				mgr := security.NewManager(cfg)

				// Open the variable
				val, err := mgr.GetSecret(ctx, "S1")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("v1"))

				// Close releases the variable and evicts the cache
				Expect(mgr.Close()).To(Succeed())

				// Next call re-opens the variable (cache was evicted)
				val, err = mgr.GetSecret(ctx, "S1")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("v1"))

				// Clean up
				Expect(mgr.Close()).To(Succeed())
			})

			It("should still allow env fallback after Close", func() {
				os.Setenv("AFTER_CLOSE_SECRET", "still-works")
				defer os.Unsetenv("AFTER_CLOSE_SECRET")

				mgr := security.NewManager(security.Config{})
				Expect(mgr.Close()).To(Succeed())

				// Env fallback doesn't depend on runtimevar
				val, err := mgr.GetSecret(ctx, "AFTER_CLOSE_SECRET")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("still-works"))
			})
		})

		Context("with multiple secrets from different URLs", func() {
			It("should resolve each independently", func() {
				cfg := security.Config{
					Secrets: map[string]string{
						"KEY_A": "constant://?val=alpha&decoder=string",
						"KEY_B": "constant://?val=bravo&decoder=string",
						"KEY_C": "constant://?val=charlie&decoder=string",
					},
				}
				mgr := security.NewManager(cfg)
				defer mgr.Close()

				a, err := mgr.GetSecret(ctx, "KEY_A")
				Expect(err).ToNot(HaveOccurred())
				Expect(a).To(Equal("alpha"))

				b, err := mgr.GetSecret(ctx, "KEY_B")
				Expect(err).ToNot(HaveOccurred())
				Expect(b).To(Equal("bravo"))

				c, err := mgr.GetSecret(ctx, "KEY_C")
				Expect(err).ToNot(HaveOccurred())
				Expect(c).To(Equal("charlie"))
			})
		})

		Context("with nil secrets map", func() {
			It("should treat all lookups as env fallback", func() {
				os.Setenv("NIL_MAP_SECRET", "env-val")
				defer os.Unsetenv("NIL_MAP_SECRET")

				mgr := security.NewManager(security.Config{})
				defer mgr.Close()

				val, err := mgr.GetSecret(ctx, "NIL_MAP_SECRET")
				Expect(err).ToNot(HaveOccurred())
				Expect(val).To(Equal("env-val"))
			})
		})
	})
})
