package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/browser"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/iacgen/generator"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/tools/email"
	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/scm"
	"github.com/appcd-dev/genie/pkg/tools/secops"
	"github.com/appcd-dev/genie/pkg/tools/websearch"
	"gopkg.in/yaml.v3"
)

type GenieConfig struct {
	ModelConfig  modelprovider.ModelConfig `yaml:"model_config" toml:"model_config"`
	Ops          generator.OpsConfig       `yaml:"ops" toml:"ops"`
	SecOps       secops.SecOpsConfig       `yaml:"secops" toml:"secops"`
	SkillsRoots  []string                  `yaml:"skills_roots" toml:"skills_roots"` // Supports multiple roots including HTTPS URLs
	MCP          mcp.MCPConfig             `yaml:"mcp" toml:"mcp"`
	WebSearch    websearch.Config          `yaml:"web_search" toml:"web_search"`
	VectorMemory vector.Config             `yaml:"vector_memory" toml:"vector_memory"`
	Messenger    messenger.Config          `yaml:"messenger" toml:"messenger"`
	Browser      browser.Config            `yaml:"browser" toml:"browser"`
	SCM          scm.Config                `yaml:"scm" toml:"scm"`
	PM           pm.Config                 `yaml:"pm" toml:"pm"`
	Email        email.Config              `yaml:"email" toml:"email"`
	AGUI         agui.ServerConfig         `yaml:"agui" toml:"agui"`
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
		WebSearch: websearch.Config{
			Provider:     os.Getenv("GENIE_SEARCH_PROVIDER"),
			GoogleAPIKey: os.Getenv("GOOGLE_API_KEY"),
			GoogleCX:     os.Getenv("GOOGLE_CSE_ID"),
			BingAPIKey:   os.Getenv("BING_API_KEY"),
		},
		VectorMemory: vector.Config{
			EmbeddingProvider: "dummy", // Default
			APIKey:            os.Getenv("OPENAI_API_KEY"),
		},
		AGUI: agui.DefaultServerConfig(),
	}

	// Override VectorMemory provider default if env vars present
	if cfg.VectorMemory.APIKey != "" {
		cfg.VectorMemory.EmbeddingProvider = "openai"
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

	// Final checks to ensure env vars override loaded config if empty in config but present in env?
	// The `os.ExpandEnv` above handles ${VAR} inside the config file.
	// But `LoadGenieConfig` logic usually means "Config File > Defaults".
	// Defaults are populated from Env Vars in the initialization above.
	// If a config file is loaded, `yaml.Unmarshal` overwrites fields.
	// If the config file does NOT specify them, the defaults (from Env) remain thanks to zero value check?
	// No, `Unmarshal` starts with the struct `cfg`.
	// If a field is missing in YAML, it keeps the existing value in `cfg`.
	// So:
	// 1. cfg init with env vars
	// 2. Unmarshal applies file values over it
	// Result: Config File > Env Vars. This is correct precedence.

	// If skills roots not set in config, check environment variable
	if len(cfg.SkillsRoots) == 0 {
		if skillsRoot := os.Getenv("SKILLS_ROOT"); skillsRoot != "" {
			cfg.SkillsRoots = []string{skillsRoot}
		}
	}

	return cfg, nil
}
