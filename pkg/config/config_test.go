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
})
