// Package pii provides PII redaction for text before storage in memory
// and vector stores. It delegates to pii-shield's entropy-based scanner
// which combines Shannon entropy analysis, English bigram scoring,
// Luhn credit card validation, context-aware key detection, and
// deterministic HMAC hashing — significantly more robust than static regex.
package pii

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"github.com/aragossa/pii-shield/pkg/scanner"
)

// Config holds PII redaction configuration that maps to pii-shield's
// scanner.Config. Only the fields that make sense for application-level
// tuning are exposed. Advanced internal fields (bigram scores, adaptive
// baseline samples) are left at their defaults.
type Config struct {
	// Salt is the HMAC key used for deterministic hashing of redacted values.
	// Same input + same salt → same [HIDDEN:hash] output, enabling log
	// correlation without exposing PII. Must be ≥16 bytes for security.
	// If empty, a cryptographically random salt is generated at startup
	// (hashes will differ across restarts).
	Salt string `yaml:"salt,omitempty" toml:"salt,omitempty"`

	// EntropyThreshold is the Shannon entropy score above which a token is
	// considered a potential secret. Lower = more aggressive (more redaction,
	// more false positives). Higher = more permissive. Default: 4.2.
	// Range: 2.0 (very aggressive) to 5.0 (very permissive).
	EntropyThreshold float64 `yaml:"entropy_threshold,omitempty" toml:"entropy_threshold,omitempty,omitzero"`

	// MinSecretLength is the minimum character length for a token to be
	// considered as a potential secret. Tokens shorter than this are never
	// redacted (unless they are values of sensitive keys). Default: 12.
	MinSecretLength int `yaml:"min_secret_length,omitempty" toml:"min_secret_length,omitempty,omitzero"`

	// SensitiveKeys is a list of key names whose values should always be
	// redacted regardless of entropy score. Case-insensitive matching.
	// Default: ["pass", "secret", "token", "key", "cvv", "cvc", "auth",
	//           "sign", "password", "passwd", "api_key", "apikey",
	//           "access_token", "client_secret"]
	SensitiveKeys []string `yaml:"sensitive_keys,omitempty" toml:"sensitive_keys,omitempty"`

	// CustomRegexes is a list of custom regex patterns for deterministic
	// PII detection. Each rule has a pattern and a name. Matched tokens
	// are redacted as [HIDDEN:name].
	// Example: [{"pattern": "\\bGHSA-[A-Za-z0-9-]+\\b", "name": "github_advisory"}]
	CustomRegexes []CustomRegexRule `yaml:"custom_regexes,omitempty" toml:"custom_regexes,omitempty"`

	// SafeRegexes is a allowlist of regex patterns. Tokens matching any
	// of these are never redacted, even if they exceed the entropy threshold.
	// Useful for known-safe patterns like version strings or build hashes.
	SafeRegexes []CustomRegexRule `yaml:"safe_regexes,omitempty" toml:"safe_regexes,omitempty"`
}

// CustomRegexRule represents a named regex pattern for PII detection.
type CustomRegexRule struct {
	Pattern string `yaml:"pattern,omitempty" toml:"pattern,omitempty" json:"pattern"`
	Name    string `yaml:"name,omitempty" toml:"name,omitempty" json:"name"`
}

// DefaultConfig returns a PII config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		EntropyThreshold: 4.2,
		MinSecretLength:  12,
	}
}

// Apply pushes this config into pii-shield's global scanner state.
// Should be called once during application startup, after config loading.
// If fields are zero-valued, pii-shield's defaults are preserved.
func (c Config) Apply() {
	cfg := scanner.Config{
		EntropyThreshold:   scanner.DefaultEntropyThreshold,
		MinSecretLength:    6,
		DisableBigramCheck: false,
		BigramDefaultScore: -7.0,
		SensitiveKeys: []string{
			"pass", "secret", "token", "key", "cvv", "cvc", "auth", "sign",
			"password", "passwd", "api_key", "apikey", "access_token", "client_secret",
		},
	}

	// Salt: use config value, or generate a fresh random one.
	if c.Salt != "" {
		cfg.Salt = []byte(c.Salt)
	} else {
		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			panic(fmt.Sprintf("pii: failed to generate random salt: %v", err))
		}
		cfg.Salt = salt
	}

	if c.EntropyThreshold > 0 {
		cfg.EntropyThreshold = c.EntropyThreshold
	}
	if c.MinSecretLength > 0 {
		cfg.MinSecretLength = c.MinSecretLength
	}
	if len(c.SensitiveKeys) > 0 {
		normalized := make([]string, len(c.SensitiveKeys))
		for i, k := range c.SensitiveKeys {
			normalized[i] = strings.ToLower(strings.TrimSpace(k))
		}
		cfg.SensitiveKeys = normalized
	}

	// Compile custom regexes for deterministic PII detection.
	for _, rule := range c.CustomRegexes {
		compiled, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue // Skip invalid patterns silently.
		}
		cfg.CustomRegexes = append(cfg.CustomRegexes, scanner.CustomRegexRule{
			Regexp: compiled,
			Name:   rule.Name,
		})
	}

	// Compile safe regexes (allowlist).
	for _, rule := range c.SafeRegexes {
		compiled, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		cfg.SafeRegexes = append(cfg.SafeRegexes, scanner.CustomRegexRule{
			Regexp: compiled,
			Name:   rule.Name,
		})
	}

	scanner.UpdateConfig(cfg)
}

// Redact replaces PII and secrets in text with deterministic HMAC hashes.
func Redact(text string) string {
	if text == "" {
		return ""
	}
	return scanner.ScanAndRedact(text)
}

// placeholderRe matches [HIDDEN:hex6] tokens produced by pii-shield's HMAC redaction.
var placeholderRe = regexp.MustCompile(`\[HIDDEN:[0-9a-f]{6}\]`)

// RedactWithReplacer redacts PII in text and returns the redacted string plus
// a *strings.Replacer that can reverse individual [HIDDEN:hash] → original
// mappings. Call replacer.Replace(llmOutput) in AfterModel to rehydrate.
//
// It works by diffing the original and redacted texts positionally: both are
// split on whitespace/punctuation boundaries and matched token-by-token.
// Where a token changed to a [HIDDEN:*] placeholder, that mapping is recorded.
func RedactWithReplacer(text string) (redacted string, replacer *strings.Replacer) {
	redacted, pairs := RedactWithPairs(text)
	if len(pairs) == 0 {
		return redacted, strings.NewReplacer()
	}
	return redacted, strings.NewReplacer(pairs...)
}

// RedactWithPairs redacts PII and returns the redacted string plus
// (placeholder, original) pairs so callers can merge multiple messages' pairs
// into one Replacer for rehydration. Pairs are old1, new1, old2, new2, ...
func RedactWithPairs(text string) (redacted string, pairs []string) {
	if text == "" {
		return "", nil
	}
	redacted = scanner.ScanAndRedact(text)
	if redacted == text {
		return text, nil
	}

	matches := placeholderRe.FindAllStringIndex(redacted, -1)
	if len(matches) == 0 {
		return redacted, []string{redacted, text}
	}

	var out []string
	seen := make(map[string]bool)

	origIdx := 0
	redIdx := 0

	for i, m := range matches {
		phStart, phEnd := m[0], m[1]
		placeholder := redacted[phStart:phEnd]

		// 1. Literal string before this placeholder
		literal := redacted[redIdx:phStart]

		if literal != "" {
			if origIdx < len(text) && strings.HasPrefix(text[origIdx:], literal) {
				origIdx += len(literal)
			} else if origIdx < len(text) {
				idx := strings.Index(text[origIdx:], literal)
				if idx >= 0 {
					origIdx += idx + len(literal)
				} else {
					origIdx += len(literal)
				}
			} else {
				origIdx += len(literal)
			}
		}

		if origIdx > len(text) {
			origIdx = len(text)
		}

		// 2. Literal string that follows this placeholder
		var nextLiteral string
		if i+1 < len(matches) {
			nextLiteral = redacted[phEnd:matches[i+1][0]]
		} else {
			nextLiteral = redacted[phEnd:]
		}

		var tokenEnd int
		if nextLiteral == "" {
			if i+1 == len(matches) {
				tokenEnd = len(text)
			} else {
				tokenEnd = origIdx
				for tokenEnd < len(text) && text[tokenEnd] != ' ' && text[tokenEnd] != '\n' && text[tokenEnd] != '\t' && text[tokenEnd] != '\r' {
					tokenEnd++
				}
			}
		} else {
			idx := strings.Index(text[origIdx:], nextLiteral)
			if idx >= 0 {
				tokenEnd = origIdx + idx
			} else {
				tokenEnd = len(text)
			}
		}

		origToken := text[origIdx:tokenEnd]

		if origToken != "" && !seen[placeholder] {
			out = append(out, placeholder, origToken)
			seen[placeholder] = true
		}

		origIdx = tokenEnd
		redIdx = phEnd
	}

	if len(out) == 0 {
		return redacted, []string{redacted, text}
	}
	return redacted, out
}

// ContainsPII returns true if redaction would modify the text.
func ContainsPII(text string) bool {
	if text == "" {
		return false
	}
	return scanner.ScanAndRedact(text) != text
}

// RedactMap applies redaction to all string values in a metadata map.
func RedactMap(metadata map[string]string) map[string]string {
	result := make(map[string]string, len(metadata))
	for k, v := range metadata {
		result[k] = Redact(v)
	}
	return result
}

// Mask replaces the middle of a string with asterisks, keeping the first
// and last n characters visible.
func Mask(s string, keepChars int) string {
	if len(s) <= keepChars*2 {
		return strings.Repeat("*", len(s))
	}
	return s[:keepChars] + strings.Repeat("*", len(s)-keepChars*2) + s[len(s)-keepChars:]
}
