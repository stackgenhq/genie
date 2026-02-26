/*
Copyright © 2026 StackGen, Inc.
*/

package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BuildGenieConfig and EncodeTOML", func() {
	It("emits default state with model_config, skills_roots, messenger.agui", func() {
		in := DefaultWizardInputs()
		cfg := BuildGenieConfig(in, nil, nil)
		var buf bytes.Buffer
		err := EncodeTOML(&buf, cfg)
		Expect(err).NotTo(HaveOccurred())
		toml := buf.String()
		Expect(toml).To(ContainSubstring("[[model_config.providers]]"))
		Expect(toml).To(ContainSubstring("provider = \"openai\""))
		Expect(toml).To(ContainSubstring("model_name = \"gpt-5.2\""))
		Expect(toml).To(ContainSubstring("${OPENAI_API_KEY}"))
		Expect(toml).To(ContainSubstring("skills_roots"))
		Expect(toml).To(ContainSubstring("\"./skills\""))
		Expect(toml).To(ContainSubstring("[messenger.agui]"))
		Expect(toml).To(ContainSubstring("port = 8080"))
	})

	It("emits [messenger] and platform block when platform is set", func() {
		in := DefaultWizardInputs()
		in.Platform = "telegram"
		in.TelegramTokenEnv = "TELEGRAM_BOT_TOKEN"
		cfg := BuildGenieConfig(in, nil, nil)
		var buf bytes.Buffer
		err := EncodeTOML(&buf, cfg)
		Expect(err).NotTo(HaveOccurred())
		toml := buf.String()
		Expect(toml).To(ContainSubstring("[messenger]"))
		Expect(toml).To(ContainSubstring("platform = \"telegram\""))
		Expect(toml).To(ContainSubstring("[messenger.telegram]"))
		Expect(toml).To(ContainSubstring("${TELEGRAM_BOT_TOKEN}"))
		Expect(toml).To(ContainSubstring("[messenger.agui]"))
	})

	It("emits Slack env placeholders when platform is slack", func() {
		in := DefaultWizardInputs()
		in.Platform = "slack"
		in.SlackAppTokenEnv = "SLACK_APP_TOKEN"
		in.SlackBotTokenEnv = "SLACK_BOT_TOKEN"
		cfg := BuildGenieConfig(in, nil, nil)
		var buf bytes.Buffer
		err := EncodeTOML(&buf, cfg)
		Expect(err).NotTo(HaveOccurred())
		toml := buf.String()
		Expect(toml).To(ContainSubstring("platform = \"slack\""))
		Expect(toml).To(ContainSubstring("[messenger.slack]"))
		Expect(toml).To(ContainSubstring("${SLACK_APP_TOKEN}"))
		Expect(toml).To(ContainSubstring("${SLACK_BOT_TOKEN}"))
	})

	It("produces valid TOML with model_config, skills_roots, and messenger.agui", func() {
		in := DefaultWizardInputs()
		cfg := BuildGenieConfig(in, nil, nil)
		var buf bytes.Buffer
		err := EncodeTOML(&buf, cfg)
		Expect(err).NotTo(HaveOccurred())
		toml := buf.String()
		Expect(strings.Contains(toml, "[[model_config.providers]]")).To(BeTrue())
		Expect(strings.Contains(toml, "skills_roots")).To(BeTrue())
		Expect(strings.Contains(toml, "[messenger.agui]")).To(BeTrue())
	})

	It("uses custom model provider token env and skills roots", func() {
		in := DefaultWizardInputs()
		in.ModelProviderTokenEnv = "MY_API_KEY"
		in.SkillsRoots = []string{"path/with/slashes"}
		cfg := BuildGenieConfig(in, nil, nil)
		var buf bytes.Buffer
		err := EncodeTOML(&buf, cfg)
		Expect(err).NotTo(HaveOccurred())
		toml := buf.String()
		Expect(toml).To(ContainSubstring("${MY_API_KEY}"))
		Expect(toml).To(ContainSubstring("path/with/slashes"))
	})

	It("omits empty string values so defaultConfig can take their place", func() {
		in := DefaultWizardInputs()
		cfg := BuildGenieConfig(in, nil, nil)
		var buf bytes.Buffer
		err := EncodeTOML(&buf, cfg)
		Expect(err).NotTo(HaveOccurred())
		toml := buf.String()
		Expect(toml).NotTo(ContainSubstring(` = ""`), "generated TOML should not contain empty string assignments")
		// BurntSushi/toml does not support omitzero; zero integers may appear in output.
	})
})

var _ = Describe("SecretNameForProvider", func() {
	It("returns OPENAI_API_KEY for openai", func() {
		Expect(SecretNameForProvider("openai")).To(Equal("OPENAI_API_KEY"))
	})
	It("returns GOOGLE_API_KEY for gemini", func() {
		Expect(SecretNameForProvider("gemini")).To(Equal("GOOGLE_API_KEY"))
	})
	It("returns ANTHROPIC_API_KEY for anthropic", func() {
		Expect(SecretNameForProvider("anthropic")).To(Equal("ANTHROPIC_API_KEY"))
	})
})

var _ = Describe("WriteConfigFile with secrets", func() {
	It("writes pasted key to configDir/secrets and emits [security.secrets] with filevar URL", func() {
		dir, err := os.MkdirTemp("", "genie-setup-secrets-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(dir)
		configPath := filepath.Join(dir, "genie.toml")
		in := DefaultWizardInputs()
		in.ModelProvider = "openai"
		in.ModelProviderTokenLiteral = "sk-secret-key"
		err = WriteConfigFile(configPath, in, nil)
		Expect(err).NotTo(HaveOccurred())
		secretsDir := filepath.Join(dir, "secrets")
		keyPath := filepath.Join(secretsDir, "OPENAI_API_KEY")
		keyData, err := os.ReadFile(keyPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(keyData)).To(Equal("sk-secret-key"))
		configData, err := os.ReadFile(configPath)
		Expect(err).NotTo(HaveOccurred())
		configStr := string(configData)
		Expect(configStr).To(ContainSubstring("${OPENAI_API_KEY}"))
		Expect(configStr).To(ContainSubstring("[security.secrets]"))
		Expect(configStr).NotTo(ContainSubstring("sk-secret-key"))
		Expect(configStr).To(ContainSubstring("file://"))
		Expect(configStr).To(ContainSubstring("decoder=string"))
	})
})
