package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/iacgen/generator"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/tools/secops"
	"gopkg.in/yaml.v3"
)

type GenieConfig struct {
	ModelConfig modelprovider.ModelConfig `yaml:"model_config" toml:"model_config"`
	Architect   generator.ArchitectConfig `yaml:"architect" toml:"architect"`
	Ops         generator.OpsConfig       `yaml:"ops" toml:"ops"`
	SecOps      secops.SecOpsConfig       `yaml:"secops" toml:"secops"`
	SkillsRoots []string                  `yaml:"skills_roots" toml:"skills_roots"` // Supports multiple roots including HTTPS URLs
	MCP         mcp.MCPConfig             `yaml:"mcp" toml:"mcp"`
}

func LoadGenieConfig(path string) (GenieConfig, error) {
	// Start with defaults
	cfg := GenieConfig{
		ModelConfig: modelprovider.DefaultModelConfig(),
		Ops: generator.OpsConfig{
			MaxPages:            5,
			EnableVerification:  true,
			MaxVerificationRuns: 3,
		},
		SecOps: secops.SecOpsConfig{
			SeverityThresholds: secops.SeverityThresholds{
				High:   0,
				Medium: 42, // Default magic number
				Low:    -1, // Unlimited
			},
		},
	}

	if path == "" {
		// If no config file, check for SKILLS_ROOT environment variable
		if skillsRoot := os.Getenv("SKILLS_ROOT"); skillsRoot != "" {
			cfg.SkillsRoots = []string{skillsRoot}
		}
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return GenieConfig{}, fmt.Errorf("failed to read config file: %w", err)
	}

	data = []byte(os.ExpandEnv(string(data)))

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return GenieConfig{}, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".toml":
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return GenieConfig{}, fmt.Errorf("failed to parse TOML config: %w", err)
		}
	default:
		return GenieConfig{}, fmt.Errorf("unsupported config file extension: %s", ext)
	}

	// If skills roots not set in config, check environment variable
	if len(cfg.SkillsRoots) == 0 {
		if skillsRoot := os.Getenv("SKILLS_ROOT"); skillsRoot != "" {
			cfg.SkillsRoots = []string{skillsRoot}
		}
	}

	return cfg, nil
}
