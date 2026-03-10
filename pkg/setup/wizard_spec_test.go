// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ApplyCollectedToInputs", func() {
	It("maps collected answers to WizardInputs", func() {
		c := &Collected{
			ConfigPath:    "/home/user/.genie.toml",
			Platform:      "telegram",
			TelegramToken: "123456:ABC-DEFsecret",
			ModelProvider: "openai",
			APIKey:        "sk-secret",
			SkillsRoots:   "./skills",
		}
		in := ApplyCollectedToInputs(c, "")
		Expect(in.Platform).To(Equal("telegram"))
		Expect(in.TelegramTokenLiteral).To(Equal("123456:ABC-DEFsecret"))
		Expect(in.ModelProvider).To(Equal("openai"))
		Expect(in.ModelProviderTokenLiteral).To(Equal("sk-secret"))
		Expect(in.SkillsRoots).To(Equal([]string{"./skills"}))
	})
	It("defaults TelegramTokenEnv when platform is telegram and no token given", func() {
		c := &Collected{Platform: "telegram"}
		in := ApplyCollectedToInputs(c, "")
		Expect(in.TelegramTokenLiteral).To(BeEmpty())
		Expect(in.TelegramTokenEnv).To(Equal("TELEGRAM_BOT_TOKEN"))
	})
	It("uses env placeholder when api_key is empty", func() {
		c := &Collected{
			Platform:      DefaultPlatform,
			ModelProvider: "gemini",
			SkillsRoots:   "./skills",
		}
		in := ApplyCollectedToInputs(c, "gemini")
		Expect(in.ModelProviderTokenLiteral).To(BeEmpty())
		Expect(in.ModelProviderTokenEnv).To(Equal("GOOGLE_API_KEY"))
	})
	It("sets ManageGoogleServices when manage_google_services is yes", func() {
		c := &Collected{ManageGoogleServices: ChoiceYes}
		in := ApplyCollectedToInputs(c, "")
		Expect(in.ManageGoogleServices).To(BeTrue())
	})
	It("leaves ManageGoogleServices false when manage_google_services is no or missing", func() {
		in := ApplyCollectedToInputs(&Collected{ManageGoogleServices: ChoiceNo}, "")
		Expect(in.ManageGoogleServices).To(BeFalse())
		in = ApplyCollectedToInputs(&Collected{}, "")
		Expect(in.ManageGoogleServices).To(BeFalse())
	})
	It("sets Learn when learn is yes", func() {
		c := &Collected{Learn: ChoiceYes}
		in := ApplyCollectedToInputs(c, "")
		Expect(in.Learn).To(BeTrue())
	})
	It("leaves Learn false when learn is no or missing", func() {
		in := ApplyCollectedToInputs(&Collected{Learn: ChoiceNo}, "")
		Expect(in.Learn).To(BeFalse())
		in = ApplyCollectedToInputs(&Collected{}, "")
		Expect(in.Learn).To(BeFalse())
	})
	It("parses data_sources_keywords comma-separated and trims to max 10", func() {
		c := &Collected{DataSourcesKeywords: " Acme , Q4 , onboarding "}
		in := ApplyCollectedToInputs(c, "")
		Expect(in.DataSourceKeywords).To(Equal([]string{"Acme", "Q4", "onboarding"}))
		c.DataSourcesKeywords = "a,b,c,d,e,f,g,h,i,j,k"
		in = ApplyCollectedToInputs(c, "")
		Expect(in.DataSourceKeywords).To(HaveLen(10))
		Expect(in.DataSourceKeywords[0]).To(Equal("a"))
		Expect(in.DataSourceKeywords[9]).To(Equal("j"))
	})
	It("sets AGUIPasswordProtected when collected has it true", func() {
		in := ApplyCollectedToInputs(&Collected{AGUIPasswordProtected: true}, "")
		Expect(in.AGUIPasswordProtected).To(BeTrue())
	})
	It("leaves AGUIPasswordProtected false when not set", func() {
		in := ApplyCollectedToInputs(&Collected{}, "")
		Expect(in.AGUIPasswordProtected).To(BeFalse())
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
		c := &Collected{ConfigPath: "/tmp/genie.toml"}
		p, err := ConfigPathFromCollected(c, "/default/.genie.toml")
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
