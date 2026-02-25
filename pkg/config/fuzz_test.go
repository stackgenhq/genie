/*
Copyright © 2026 StackGen, Inc.
*/

package config

import (
	"context"
	"strings"
	"testing"

	"github.com/stackgenhq/genie/pkg/security"
)

// FuzzExpandSecrets tests expandSecrets with arbitrary input strings.
// Run with: go test -fuzz=FuzzExpandSecrets ./pkg/config/
func FuzzExpandSecrets(f *testing.F) {
	// Seed corpus with representative patterns.
	f.Add("plain text")
	f.Add("${FOO}")
	f.Add("$FOO")
	f.Add("${}")
	f.Add("${FOO}:${BAR}")
	f.Add("$$$$")
	f.Add("${VERY_LONG_" + strings.Repeat("A", 1000) + "}")
	f.Add("token = \"${API_KEY}\"")
	f.Add("nested = \"${${INNER}}\"")
	f.Add("")

	sp := security.NewEnvProvider()
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, input string) {
		// expandSecrets must never panic regardless of input.
		result := expandSecrets(ctx, sp, input)

		// Result must not be longer than the input expanded with all
		// possible env vars (since unresolved vars become "").
		// At minimum, it must not panic.
		_ = result
	})
}

// FuzzValidateProviderToken tests providerTokenInfo.validate with arbitrary inputs.
// Run with: go test -fuzz=FuzzValidateProviderToken ./pkg/config/
func FuzzValidateProviderToken(f *testing.F) {
	f.Add("openai", "gpt-4", "", "")
	f.Add("gemini", "gemini-pro", "sk-test", "")
	f.Add("ollama", "llama3", "", "http://localhost:11434")
	f.Add("OPENAI", "model", "", "")
	f.Add("unknown", "", "", "")
	f.Add("", "", "", "")

	f.Fuzz(func(t *testing.T, provider, model, token, host string) {
		p := providerTokenInfo{
			Provider:  provider,
			ModelName: model,
			Token:     token,
			Host:      host,
		}
		// Must never panic.
		_ = p.validate()
	})
}
