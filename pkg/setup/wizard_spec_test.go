/*
Copyright © 2026 StackGen, Inc.
*/

package setup

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ApplyCollectedToInputs", func() {
	It("maps collected answers to WizardInputs", func() {
		collected := map[string]string{
			"config_path":        "/home/user/.genie.toml",
			"platform":           "telegram",
			"telegram_token_env": "TELEGRAM_BOT_TOKEN",
			"model_provider":     "openai",
			"api_key":            "sk-secret",
			"skills_roots":       "./skills",
		}
		in := ApplyCollectedToInputs(collected, "")
		Expect(in.Platform).To(Equal("telegram"))
		Expect(in.TelegramTokenEnv).To(Equal("TELEGRAM_BOT_TOKEN"))
		Expect(in.ModelProvider).To(Equal("openai"))
		Expect(in.ModelProviderTokenLiteral).To(Equal("sk-secret"))
		Expect(in.SkillsRoots).To(Equal([]string{"./skills"}))
	})
	It("uses env placeholder when api_key is empty", func() {
		collected := map[string]string{
			"platform":       "agui",
			"model_provider": "gemini",
			"skills_roots":   "./skills",
		}
		in := ApplyCollectedToInputs(collected, "gemini")
		Expect(in.ModelProviderTokenLiteral).To(BeEmpty())
		Expect(in.ModelProviderTokenEnv).To(Equal("GOOGLE_API_KEY"))
	})
	It("sets ManageGoogleServices when manage_google_services is yes", func() {
		collected := map[string]string{"manage_google_services": "yes"}
		in := ApplyCollectedToInputs(collected, "")
		Expect(in.ManageGoogleServices).To(BeTrue())
	})
	It("leaves ManageGoogleServices false when manage_google_services is no or missing", func() {
		in := ApplyCollectedToInputs(map[string]string{"manage_google_services": "no"}, "")
		Expect(in.ManageGoogleServices).To(BeFalse())
		in = ApplyCollectedToInputs(map[string]string{}, "")
		Expect(in.ManageGoogleServices).To(BeFalse())
	})
})

var _ = Describe("DetectAIKeyProvider", func() {
	It("returns a valid provider or empty", func() {
		p := DetectAIKeyProvider()
		Expect(p).To(BeElementOf("", "openai", "gemini", "anthropic"))
	})
})

var _ = Describe("ConfigPathFromCollected", func() {
	It("returns absolute path from collected or default", func() {
		collected := map[string]string{"config_path": "/tmp/genie.toml"}
		p, err := ConfigPathFromCollected(collected, "/default/.genie.toml")
		Expect(err).NotTo(HaveOccurred())
		Expect(p).To(ContainSubstring("genie.toml"))
	})
})

var _ = Describe("ValidateConfigPath", func() {
	It("returns error for empty path", func() {
		Expect(ValidateConfigPath("")).To(HaveOccurred())
	})
	It("returns no error for current dir", func() {
		Expect(ValidateConfigPath(".")).NotTo(HaveOccurred())
	})
})
