// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package modelprovider

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ModelProvider Internal", func() {
	Describe("ProviderConfigs.getForTask", func() {
		Context("when providers list is empty", func() {
			It("should return an error", func() {
				var providers ProviderConfigs
				config, usedFallback, err := providers.getForTask(TaskEfficiency)
				Expect(err).To(MatchError("no providers configured"))
				Expect(config.Providers()).To(BeEmpty())
				Expect(usedFallback).To(BeFalse())
			})
		})

		Context("when no matching provider is found", func() {
			It("should return default provider if available (logic check)", func() {
				// The current implementation iterates and returns if match found.
				// If no match found, it returns providers[0] if list is not empty.
				providers := ProviderConfigs{
					{
						Provider:    "openai",
						GoodForTask: TaskEfficiency,
					},
				}

				// Requesting a different task type
				config, usedFallback, err := providers.getForTask(TaskToolCalling)
				Expect(err).NotTo(HaveOccurred())
				// Should fallback to the first provider
				Expect(config.Providers()).To(Equal([]string{"openai"}))
				Expect(usedFallback).To(BeTrue())
			})
		})

		Context("when matching provider is found", func() {
			It("should return the matching provider without fallback", func() {
				providers := ProviderConfigs{
					{
						Provider:    "openai",
						GoodForTask: TaskEfficiency,
					},
					{
						Provider:    "gemini",
						GoodForTask: TaskToolCalling,
					},
				}

				config, usedFallback, err := providers.getForTask(TaskToolCalling)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.Providers()).To(Equal([]string{"gemini"}))
				Expect(usedFallback).To(BeFalse())
			})
		})
	})

	Describe("resolveTokenForDefaultProvider", func() {
		mockGet := func(vals map[string]string) func(string) string {
			return func(name string) string {
				return vals[name]
			}
		}

		DescribeTable("resolves tokens by provider",
			func(provider string, envVars map[string]string, expectedToken string) {
				p := ProviderConfig{Provider: provider}
				result := resolveTokenForDefaultProvider(p, mockGet(envVars))
				Expect(result.Token).To(Equal(expectedToken))
			},
			Entry("openai resolves OPENAI_API_KEY",
				"openai", map[string]string{"OPENAI_API_KEY": "sk-test123"}, "sk-test123"),
			Entry("openai with no key returns empty",
				"openai", map[string]string{}, ""),
			Entry("gemini resolves GEMINI_API_KEY first",
				"gemini", map[string]string{"GEMINI_API_KEY": "gem-key", "GOOGLE_API_KEY": "goog-key"}, "gem-key"),
			Entry("gemini falls back to GOOGLE_API_KEY",
				"gemini", map[string]string{"GOOGLE_API_KEY": "goog-key"}, "goog-key"),
			Entry("anthropic resolves ANTHROPIC_API_KEY",
				"anthropic", map[string]string{"ANTHROPIC_API_KEY": "ant-key"}, "ant-key"),
			Entry("unknown provider leaves token empty",
				"unknown", map[string]string{"UNKNOWN_KEY": "val"}, ""),
			Entry("case insensitive OpenAI",
				"OpenAI", map[string]string{"OPENAI_API_KEY": "sk-upper"}, "sk-upper"),
		)
	})
})
