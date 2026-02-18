package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/browser"
	"github.com/appcd-dev/genie/pkg/cron"
	"github.com/appcd-dev/genie/pkg/db"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/runbook"
	"github.com/appcd-dev/genie/pkg/security"
	"github.com/appcd-dev/genie/pkg/tools/email"
	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/scm"
	"github.com/appcd-dev/genie/pkg/tools/websearch"
	"gopkg.in/yaml.v3"
)

type GenieConfig struct {
	ModelConfig  modelprovider.ModelConfig `yaml:"model_config" toml:"model_config"`
	SkillsRoots  []string                  `yaml:"skills_roots" toml:"skills_roots"` // Supports multiple roots including HTTPS URLs
	MCP          mcp.MCPConfig             `yaml:"mcp" toml:"mcp"`
	WebSearch    websearch.Config          `yaml:"web_search" toml:"web_search"`
	VectorMemory vector.Config             `yaml:"vector_memory" toml:"vector_memory"`
	Messenger    messenger.Config          `yaml:"messenger" toml:"messenger"`
	Browser      browser.Config            `yaml:"browser" toml:"browser"`
	SCM          scm.Config                `yaml:"scm" toml:"scm"`

	ProjectManagement pm.Config `yaml:"project_management" toml:"project_management"`

	Email    email.Config      `yaml:"email" toml:"email"`
	AGUI     agui.ServerConfig `yaml:"agui" toml:"agui"`
	HITL     hitl.Config       `yaml:"hitl" toml:"hitl"`
	DBConfig db.Config         `yaml:"db_config" toml:"db_config"`
	Langfuse langfuse.Config   `yaml:"langfuse" toml:"langfuse"`
	Runbook  runbook.Config    `yaml:"runbook" toml:"runbook"`
	Cron     cron.Config       `yaml:"cron" toml:"cron"`
	Security security.Config   `yaml:"security" toml:"security"`
}

// LoadGenieConfig loads the Genie configuration from a file, resolving
// secret-dependent defaults through the given SecretProvider. Passing
// security.NewEnvProvider() preserves the legacy os.Getenv behavior.
func LoadGenieConfig(ctx context.Context, sp security.SecretProvider, path string) (GenieConfig, error) {
	// Helper to resolve a secret, ignoring errors (treat as empty).
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, name)
		return v
	}

	// Start with defaults
	cfg := GenieConfig{
		ModelConfig: modelprovider.DefaultModelConfig(ctx, sp),
		WebSearch: websearch.Config{
			Provider:     get("GENIE_SEARCH_PROVIDER"),
			GoogleAPIKey: get("GOOGLE_API_KEY"),
			GoogleCX:     get("GOOGLE_CSE_ID"),
			BingAPIKey:   get("BING_API_KEY"),
		},
		VectorMemory: vector.DefaultConfig(ctx, sp),
		AGUI:         agui.DefaultServerConfig(),
		HITL:         hitl.DefaultConfig(),
		DBConfig:     db.DefaultConfig(),
		Langfuse:     langfuse.DefaultConfig(ctx, sp),
	}

	// Override VectorMemory provider default if env vars present.
	// Priority: openai > gemini > huggingface.
	if cfg.VectorMemory.APIKey != "" {
		cfg.VectorMemory.EmbeddingProvider = "openai"
	}
	if cfg.VectorMemory.GeminiAPIKey != "" && cfg.VectorMemory.EmbeddingProvider == "dummy" {
		cfg.VectorMemory.EmbeddingProvider = "gemini"
	}
	if cfg.VectorMemory.HuggingFaceURL != "" && cfg.VectorMemory.EmbeddingProvider == "dummy" {
		cfg.VectorMemory.EmbeddingProvider = "huggingface"
	}

	if path == "" {
		// If no config file, check for SKILLS_ROOT environment variable
		if skillsRoot := get("SKILLS_ROOT"); skillsRoot != "" {
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
		if skillsRoot := get("SKILLS_ROOT"); skillsRoot != "" {
			cfg.SkillsRoots = []string{skillsRoot}
		}
	}

	return cfg, nil
}
