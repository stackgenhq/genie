package config_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/config"
	"github.com/appcd-dev/genie/pkg/security"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeSecretProvider returns configured values for secret names.
type fakeSecretProvider struct {
	secrets map[string]string
}

func (f *fakeSecretProvider) GetSecret(_ context.Context, name string) (string, error) {
	return f.secrets[name], nil
}

var _ = Describe("LoadGenieConfig", func() {
	var (
		ctx           context.Context
		sp            security.SecretProvider
		validYamlPath string
		validTomlPath string
		invalidPath   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		sp = security.NewEnvProvider()
		validYamlPath = filepath.Join("testdata", "valid.yaml")
		validTomlPath = filepath.Join("testdata", "valid.toml")
		invalidPath = filepath.Join("testdata", "invalid.yaml")
	})

	It("should load values from YAML file", func() {
		cfg, err := config.LoadGenieConfig(ctx, sp, validYamlPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers).To(HaveLen(1))
	})

	It("should load values from TOML file", func() {
		cfg, err := config.LoadGenieConfig(ctx, sp, validTomlPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers).To(HaveLen(1))
	})

	It("should error when file content is invalid", func() {
		_, err := config.LoadGenieConfig(ctx, sp, invalidPath)
		Expect(err).To(HaveOccurred())
	})

	It("should expand environment variables", func() {
		os.Setenv("TEST_PROVIDER", "openai")
		defer os.Unsetenv("TEST_PROVIDER")

		path := filepath.Join("testdata", "env_vars.yaml")
		cfg, err := config.LoadGenieConfig(ctx, sp, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers[0].Provider).To(Equal("openai"))
	})

	It("should return error for unsupported file extensions", func() {
		tmpDir := GinkgoT().TempDir()
		jsonFile := filepath.Join(tmpDir, "config.json")
		err := os.WriteFile(jsonFile, []byte(`{}`), 0644)
		Expect(err).NotTo(HaveOccurred())

		_, err = config.LoadGenieConfig(ctx, sp, jsonFile)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported config file extension"))
	})

	It("should set SKILLS_ROOT from env when path is empty", func() {
		fakeSP := &fakeSecretProvider{
			secrets: map[string]string{
				"SKILLS_ROOT": "/tmp/my-skills",
			},
		}
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SkillsRoots).To(ContainElement("/tmp/my-skills"))
	})

	It("should set SKILLS_ROOT from env when config file has empty skills_roots", func() {
		// Create a minimal config file without skills_roots
		tmpDir := GinkgoT().TempDir()
		cfgFile := filepath.Join(tmpDir, "config.yaml")
		err := os.WriteFile(cfgFile, []byte("model_config:\n  providers: []\n"), 0644)
		Expect(err).NotTo(HaveOccurred())

		fakeSP := &fakeSecretProvider{
			secrets: map[string]string{
				"SKILLS_ROOT": "/tmp/fallback-skills",
			},
		}
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, cfgFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SkillsRoots).To(ContainElement("/tmp/fallback-skills"))
	})

	It("should set VectorMemory embedding provider to gemini when gemini key is set", func() {
		fakeSP := &fakeSecretProvider{
			secrets: map[string]string{
				"GOOGLE_API_KEY": "test-gemini-key",
			},
		}
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.VectorMemory.EmbeddingProvider).To(Equal("gemini"))
	})

	It("should set VectorMemory embedding provider to huggingface when HF URL is set", func() {
		fakeSP := &fakeSecretProvider{
			secrets: map[string]string{
				"HUGGINGFACE_URL": "http://localhost:8080",
			},
		}
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.VectorMemory.EmbeddingProvider).To(Equal("huggingface"))
	})

	It("should prioritize openai over gemini for embedding provider", func() {
		fakeSP := &fakeSecretProvider{
			secrets: map[string]string{
				"OPENAI_API_KEY": "test-openai-key",
				"GEMINI_API_KEY": "test-gemini-key",
			},
		}
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.VectorMemory.EmbeddingProvider).To(Equal("openai"))
	})

	It("should return error for invalid TOML content", func() {
		tmpDir := GinkgoT().TempDir()
		badToml := filepath.Join(tmpDir, "bad.toml")
		err := os.WriteFile(badToml, []byte(`this is not valid toml {{{`), 0644)
		Expect(err).NotTo(HaveOccurred())

		_, err = config.LoadGenieConfig(ctx, sp, badToml)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse TOML"))
	})

	It("should resolve ${VAR} via SecretProvider without env var", func() {
		// Ensure the env var is NOT set so the only way to resolve is the provider.
		os.Unsetenv("MY_TOKEN")

		fakeSP := &fakeSecretProvider{
			secrets: map[string]string{
				"MY_TOKEN": "resolved-from-provider",
			},
		}
		path := filepath.Join("testdata", "secret_provider.yaml")
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers).To(HaveLen(1))
		Expect(cfg.ModelConfig.Providers[0].Token).To(Equal("resolved-from-provider"))
	})

	It("should fall back to env var via NewEnvProvider", func() {
		os.Setenv("TEST_PROVIDER", "openai")
		defer os.Unsetenv("TEST_PROVIDER")

		// NewEnvProvider resolves secrets from env vars — the default
		// path when no [security.secrets] section is configured.
		envSP := security.NewEnvProvider()

		path := filepath.Join("testdata", "env_vars.yaml")
		cfg, err := config.LoadGenieConfig(ctx, envSP, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers[0].Provider).To(Equal("openai"))
	})

	It("should resolve typo'd placeholder to empty without error", func() {
		// Ensures that a typo like ${OPENAI_APY_KEY} does not cause a hard error.
		tmpDir := GinkgoT().TempDir()
		cfgFile := filepath.Join(tmpDir, "typo.yaml")
		content := "model_config:\n  providers:\n    - provider: openai\n      token: \"${OPENAI_APY_KEY}\"\n"
		err := os.WriteFile(cfgFile, []byte(content), 0644)
		Expect(err).NotTo(HaveOccurred())

		os.Unsetenv("OPENAI_APY_KEY")
		fakeSP := &fakeSecretProvider{secrets: map[string]string{}}
		cfg, err := config.LoadGenieConfig(ctx, fakeSP, cfgFile)
		Expect(err).ToNot(HaveOccurred())
		// Token resolves to empty — the warning is logged but not an error.
		Expect(cfg.ModelConfig.Providers[0].Token).To(BeEmpty())
	})
})
