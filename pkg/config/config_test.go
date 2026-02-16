package config_test

import (
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LoadGenieConfig", func() {
	var (
		validYamlPath string
		validTomlPath string
		invalidPath   string
	)

	BeforeEach(func() {
		validYamlPath = filepath.Join("testdata", "valid.yaml")
		validTomlPath = filepath.Join("testdata", "valid.toml")
		invalidPath = filepath.Join("testdata", "invalid.yaml")
	})

	It("should load values from YAML file", func() {
		cfg, err := config.LoadGenieConfig(validYamlPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers).To(HaveLen(1))
	})

	It("should load values from TOML file", func() {
		cfg, err := config.LoadGenieConfig(validTomlPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers).To(HaveLen(1))
	})

	It("should error when file content is invalid", func() {
		_, err := config.LoadGenieConfig(invalidPath)
		Expect(err).To(HaveOccurred())
	})

	It("should expand environment variables", func() {
		os.Setenv("TEST_PROVIDER", "openai")
		defer os.Unsetenv("TEST_PROVIDER")

		path := filepath.Join("testdata", "env_vars.yaml")
		cfg, err := config.LoadGenieConfig(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.ModelConfig.Providers[0].Provider).To(Equal("openai"))
	})
})
