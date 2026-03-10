// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package setup: detect checks for existing AI API keys in the environment
// so the wizard can pre-select a provider and optionally skip the "paste key" step.
package setup

import (
	"os"
	"strings"
)

// Env var names used by Genie for each provider (must match SecretNameForProvider).
const (
	EnvOpenAIKey    = "OPENAI_API_KEY"
	EnvGoogleKey    = "GOOGLE_API_KEY"
	EnvAnthropicKey = "ANTHROPIC_API_KEY"
)

// DetectAIKeyProvider returns the first provider that has a non-empty API key
// in the environment: "openai", "gemini", "anthropic", or "" if none set.
// Use this before the model step to pre-select the provider and allow
// "leave blank to use existing key".
func DetectAIKeyProvider() string {
	if v := os.Getenv(EnvOpenAIKey); strings.TrimSpace(v) != "" {
		return "openai"
	}
	if v := os.Getenv(EnvGoogleKey); strings.TrimSpace(v) != "" {
		return "gemini"
	}
	if v := os.Getenv(EnvAnthropicKey); strings.TrimSpace(v) != "" {
		return "anthropic"
	}
	return ""
}
