package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/stackgenhq/genie/pkg/browser"
	"github.com/stackgenhq/genie/pkg/cron"
	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/langfuse"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/pii"
	"github.com/stackgenhq/genie/pkg/runbook"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/email"
	"github.com/stackgenhq/genie/pkg/tools/gdrive"
	"github.com/stackgenhq/genie/pkg/tools/pm"
	"github.com/stackgenhq/genie/pkg/tools/scm"
	"github.com/stackgenhq/genie/pkg/tools/websearch"
	"github.com/stackgenhq/genie/pkg/toolwrap"
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

	Email    email.Config              `yaml:"email" toml:"email"`
	GDrive   gdrive.Config             `yaml:"google_drive" toml:"google_drive"`
	HITL     hitl.Config               `yaml:"hitl" toml:"hitl"`
	DBConfig db.Config                 `yaml:"db_config" toml:"db_config"`
	Langfuse langfuse.Config           `yaml:"langfuse" toml:"langfuse"`
	Runbook  runbook.Config            `yaml:"runbook" toml:"runbook"`
	Cron     cron.Config               `yaml:"cron" toml:"cron"`
	Security security.Config           `yaml:"security" toml:"security"`
	PII      pii.Config                `yaml:"pii" toml:"pii"`
	Toolwrap toolwrap.MiddlewareConfig `yaml:"toolwrap" toml:"toolwrap"`

	// DisablePensieve disables the Pensieve context management tools
	// (delete_context, check_budget, note, read_notes) from arXiv:2602.12108.
	// When true, the agent can actively manage its own context window.
	// delete_context and note require HITL approval; check_budget and
	// read_notes are read-only and auto-approved.
	DisablePensieve bool `yaml:"disable_pensieve" toml:"disable_pensieve"`
}

// LoadGenieConfig loads the Genie configuration from a file, resolving
// secret-dependent defaults and ${VAR} placeholders through the given
// SecretProvider. Each ${NAME} occurrence in the config file is resolved
// by calling sp.GetSecret(ctx, NAME), which may consult runtimevar
// backends (GCP Secret Manager, AWS Secrets Manager, mounted files,
// etc.) or fall back to os.Getenv depending on the provider.
//
// Passing security.NewEnvProvider() preserves the legacy os.Getenv
// behavior. Passing a security.Manager created from the config's
// [security.secrets] section enables runtimevar-backed resolution.
//
// After interpolation, secret values are resolved via sp.GetSecret;
// empty or missing values are not logged here (use WarnMissingTokens or
// provider-specific validation if early surfacing of typos is needed).
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
		Messenger: messenger.Config{
			AGUI: messenger.DefaultAGUIConfig(),
		},
		HITL:     hitl.DefaultConfig(),
		DBConfig: db.DefaultConfig(),
		Langfuse: langfuse.DefaultConfig(ctx, sp),
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
	// Auto-detect Milvus if MILVUS_ADDRESS is set and vector_store_provider is not explicitly set
	if cfg.VectorMemory.Milvus.Address != "" && cfg.VectorMemory.VectorStoreProvider == "" {
		cfg.VectorMemory.VectorStoreProvider = "milvus"
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

	expanded := expandSecrets(ctx, sp, string(data))
	data = []byte(expanded)

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
		if skillsRoot := get("SKILLS_ROOT"); skillsRoot != "" {
			cfg.SkillsRoots = []string{skillsRoot}
		}
	}

	return cfg, nil
}

// WarnMissingTokens logs a warning for each model provider that typically
// requires an API key but has an empty token. Call this after the final
// config pass so that runtimevar-backed secrets have been resolved.
// Without this separation, the two-pass loading in cmd/root.go would
// emit spurious warnings during the preliminary env-only pass.
func WarnMissingTokens(ctx context.Context, cfg GenieConfig, configPath string) {
	logr := logger.GetLogger(ctx)
	for _, p := range cfg.ModelConfig.Providers {
		ptInfo := providerTokenInfo{
			Provider:  p.Provider,
			ModelName: p.ModelName,
			Token:     p.Token,
			Host:      p.Host,
		}
		if err := ptInfo.validate(); err != nil {
			logr.Warn("model provider configured without API token",
				"provider", p.Provider,
				"model", p.ModelName,
				"config_path", configPath,
				"hint", "set the token field or configure the secret in [security.secrets]",
			)
		}
	}
}
