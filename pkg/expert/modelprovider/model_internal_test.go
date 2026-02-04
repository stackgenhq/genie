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
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no providers configured"))
				Expect(config).To(Equal(ProviderConfig{}))
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
				Expect(config.Provider).To(Equal("openai"))
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
				Expect(config.Provider).To(Equal("gemini"))
				Expect(usedFallback).To(BeFalse())
			})
		})
	})
})
