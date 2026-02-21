package pii_test

import (
	"testing"

	"github.com/appcd-dev/genie/pkg/pii"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPII(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "PII Redaction Suite")
}

var _ = Describe("Redact", func() {
	It("redacts high-entropy strings (API keys)", func() {
		result := pii.Redact("token: sk-1234567890abcdef1234567890abcdef")
		Expect(result).To(ContainSubstring("[HIDDEN:"))
		Expect(result).NotTo(ContainSubstring("1234567890abcdef"))
	})

	It("redacts sensitive key=value pairs", func() {
		result := pii.Redact("password=SuperSecret123!")
		Expect(result).To(ContainSubstring("[HIDDEN:"))
		Expect(result).NotTo(ContainSubstring("SuperSecret123"))
	})

	It("preserves normal text without PII", func() {
		text := "The deployment was successful."
		Expect(pii.Redact(text)).To(Equal(text))
	})

	It("handles empty string", func() {
		Expect(pii.Redact("")).To(Equal(""))
	})

	It("redacts secret=value pairs via context-aware detection", func() {
		result := pii.Redact("secret=MyV3ryS3cr3tT0k3n!")
		Expect(result).To(ContainSubstring("[HIDDEN:"))
		Expect(result).NotTo(ContainSubstring("MyV3ryS3cr3tT0k3n"))
	})

	It("preserves normal code-like text", func() {
		text := "func main() { fmt.Println(\"hello\") }"
		result := pii.Redact(text)
		Expect(result).To(ContainSubstring("func"))
		Expect(result).To(ContainSubstring("main"))
	})

	It("produces deterministic output for same input", func() {
		input := "api_key=xK9mP2nQ5rT8wZ3v"
		result1 := pii.Redact(input)
		result2 := pii.Redact(input)
		Expect(result1).To(Equal(result2))
	})
})

var _ = Describe("ContainsPII", func() {
	It("returns true for text with secrets", func() {
		Expect(pii.ContainsPII("password=hunter2")).To(BeTrue())
	})

	It("returns false for clean text", func() {
		Expect(pii.ContainsPII("no sensitive data here")).To(BeFalse())
	})

	It("returns false for empty string", func() {
		Expect(pii.ContainsPII("")).To(BeFalse())
	})
})

var _ = Describe("RedactMap", func() {
	It("redacts all string values in a map", func() {
		input := map[string]string{
			"safe":     "hello world",
			"password": "password=s3cr3t!value",
		}
		result := pii.RedactMap(input)
		Expect(result["safe"]).To(Equal("hello world"))
		// The password key-value pair should be redacted
		Expect(result["password"]).To(ContainSubstring("[HIDDEN:"))
	})

	It("handles empty map", func() {
		result := pii.RedactMap(map[string]string{})
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("Mask", func() {
	It("masks middle characters", func() {
		Expect(pii.Mask("1234567890", 2)).To(Equal("12******90"))
	})

	It("masks everything if too short", func() {
		Expect(pii.Mask("ab", 2)).To(Equal("**"))
	})

	It("keeps specified number of edge characters", func() {
		Expect(pii.Mask("abcdefghij", 3)).To(Equal("abc****hij"))
	})

	It("handles single character keep", func() {
		Expect(pii.Mask("abcdef", 1)).To(Equal("a****f"))
	})
})

var _ = Describe("Config", func() {
	Describe("DefaultConfig", func() {
		It("returns sensible defaults", func() {
			cfg := pii.DefaultConfig()
			Expect(cfg.EntropyThreshold).To(Equal(3.6))
			Expect(cfg.MinSecretLength).To(Equal(6))
			Expect(cfg.Salt).To(BeEmpty())
			Expect(cfg.SensitiveKeys).To(BeEmpty())
			Expect(cfg.CustomRegexes).To(BeEmpty())
			Expect(cfg.SafeRegexes).To(BeEmpty())
		})
	})

	Describe("Apply", func() {
		It("does not panic with default config", func() {
			cfg := pii.DefaultConfig()
			Expect(func() { cfg.Apply() }).NotTo(Panic())
		})

		It("does not panic with custom salt", func() {
			cfg := pii.Config{
				Salt:             "my-stable-test-salt-1234",
				EntropyThreshold: 4.0,
				MinSecretLength:  8,
				SensitiveKeys:    []string{"api_key", "token", "password"},
			}
			Expect(func() { cfg.Apply() }).NotTo(Panic())
		})

		It("applies custom sensitive keys", func() {
			cfg := pii.Config{
				SensitiveKeys: []string{"my_custom_secret"},
			}
			cfg.Apply()
			// After applying, the scanner should detect our custom key
			result := pii.Redact("my_custom_secret=SuperS3cretValue!")
			Expect(result).To(ContainSubstring("[HIDDEN:"))
		})

		It("applies custom regexes", func() {
			cfg := pii.Config{
				CustomRegexes: []pii.CustomRegexRule{
					{Pattern: `\bTEST-[A-Z0-9]{10}\b`, Name: "test_id"},
				},
			}
			cfg.Apply()
			result := pii.Redact("id: TEST-ABCDEFGHIJ")
			Expect(result).To(ContainSubstring("[HIDDEN"))
		})

		It("skips invalid regex patterns without panicking", func() {
			cfg := pii.Config{
				CustomRegexes: []pii.CustomRegexRule{
					{Pattern: `[invalid`, Name: "bad"},
				},
			}
			Expect(func() { cfg.Apply() }).NotTo(Panic())
		})

		It("skips invalid safe regex patterns without panicking", func() {
			cfg := pii.Config{
				SafeRegexes: []pii.CustomRegexRule{
					{Pattern: `[invalid`, Name: "bad_safe"},
				},
			}
			Expect(func() { cfg.Apply() }).NotTo(Panic())
		})

		// Restore defaults for other tests (since scanner.UpdateConfig is global state).
		AfterEach(func() {
			pii.DefaultConfig().Apply()
		})
	})
})
