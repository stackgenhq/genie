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
		validYamlPath   string
		validTomlPath   string
		invalidPath     string
		nonExistentPath string
	)

	BeforeEach(func() {
		validYamlPath = filepath.Join("testdata", "valid.yaml")
		validTomlPath = filepath.Join("testdata", "valid.toml")
		invalidPath = filepath.Join("testdata", "invalid.yaml")
		nonExistentPath = filepath.Join("testdata", "non_existent.yaml")
	})

	It("should return default values when path is empty", func() {
		cfg, err := config.LoadGenieConfig("")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Ops.MaxPages).To(Equal(5))
		Expect(cfg.Ops.EnableVerification).To(BeTrue())
		Expect(cfg.SecOps.SeverityThresholds.Medium).To(Equal(42))
	})

	It("should load values from YAML file", func() {
		cfg, err := config.LoadGenieConfig(validYamlPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Ops.MaxPages).To(Equal(10))
		Expect(cfg.Ops.EnableVerification).To(BeFalse())
		Expect(cfg.SecOps.SeverityThresholds.Medium).To(Equal(20))
	})

	It("should load values from TOML file", func() {
		cfg, err := config.LoadGenieConfig(validTomlPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Ops.MaxPages).To(Equal(8))
		Expect(cfg.Ops.EnableVerification).To(BeTrue())
		Expect(cfg.SecOps.SeverityThresholds.Medium).To(Equal(30))
	})

	It("should return defaults when file does not exist", func() {
		cfg, err := config.LoadGenieConfig(nonExistentPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Ops.MaxPages).To(Equal(5))
	})

	It("should error when file content is invalid", func() {
		_, err := config.LoadGenieConfig(invalidPath)
		Expect(err).To(HaveOccurred())
	})

	It("should expand environment variables", func() {
		os.Setenv("TEST_MAX_PAGES", "20")
		defer os.Unsetenv("TEST_MAX_PAGES")

		path := filepath.Join("testdata", "env_vars.yaml")
		cfg, err := config.LoadGenieConfig(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Ops.MaxPages).To(Equal(20))
	})
})
