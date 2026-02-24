package modelprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/security"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpc.group/trpc-go/trpc-agent-go/model/gemini"
	"trpc.group/trpc-go/trpc-agent-go/model/huggingface"
	"trpc.group/trpc-go/trpc-agent-go/model/ollama"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

type ModelConfig struct {
	Providers ProviderConfigs `json:"providers" yaml:"providers" toml:"providers"`
}

// DefaultModelConfig builds the default model configuration by resolving
// API keys through the given SecretProvider. Each provider is added only
// if its API key is present. Without a SecretProvider, callers can pass
// security.NewEnvProvider() to preserve the legacy os.Getenv behavior.
func DefaultModelConfig(ctx context.Context, sp security.SecretProvider) ModelConfig {
	// Helper to resolve a secret, ignoring errors (treat as empty).
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, name)
		return v
	}
	getWithDefault := func(name, defaultValue string) string {
		v := get(name)
		if v == "" {
			return defaultValue
		}
		return v
	}

	// add each provider and their default model if env variables are available
	result := ModelConfig{}
	if get("OPENAI_API_KEY") != "" {
		result.Providers = append(result.Providers, ProviderConfig{
			Provider:    "openai",
			ModelName:   getWithDefault("OPENAI_MODEL", "gpt-5.2"),
			Variant:     "default",
			GoodForTask: TaskEfficiency,
		})
	}
	if get("GEMINI_API_KEY") != "" || get("GOOGLE_API_KEY") != "" {
		// Flash model for lightweight front desk classification / triage
		result.Providers = append(result.Providers, ProviderConfig{
			Provider:    "gemini",
			ModelName:   "gemini-3-flash-preview",
			GoodForTask: TaskEfficiency,
		})
		result.Providers = append(result.Providers, ProviderConfig{
			Provider:    "gemini",
			ModelName:   getWithDefault("GOOGLE_MODEL", "gemini-3-pro-preview"),
			GoodForTask: TaskToolCalling,
		})
		result.Providers = append(result.Providers, ProviderConfig{
			Provider:    "gemini",
			ModelName:   getWithDefault("GOOGLE_MODEL", "gemini-3-pro-preview"),
			GoodForTask: TaskGeneralTask,
		})
	}
	if get("ANTHROPIC_API_KEY") != "" {
		result.Providers = append(result.Providers, ProviderConfig{
			Provider:    "anthropic",
			ModelName:   getWithDefault("ANTHROPIC_MODEL", "claude-opus-4-5-20251101"),
			Variant:     "default",
			GoodForTask: TaskPlanning,
		})
	}
	return result
}

type ProviderConfigs []ProviderConfig

func (providers ProviderConfigs) Providers() []string {
	result := []string{}
	for _, provider := range providers {
		result = append(result, provider.Provider)
	}
	return result
}

func (providers ProviderConfigs) toModels(ctx context.Context) (map[string]model.Model, error) {
	models := map[string]model.Model{}
	for _, provider := range providers {
		model, err := provider.toModel(ctx)
		if err != nil {
			return nil, err
		}
		models[provider.String()] = model
	}
	return models, nil
}

func (providers ProviderConfigs) getForTask(taskType TaskType) (ProviderConfigs, bool, error) {
	result := ProviderConfigs{}
	for _, provider := range providers {
		if provider.GoodForTask == taskType {
			result = append(result, provider)
		}
	}
	if len(providers) == 0 {
		return []ProviderConfig{}, false, fmt.Errorf("no providers configured")
	}
	if len(result) == 0 {
		return providers, true, nil // fallback to single/first provider
	}
	return result, false, nil // fallback to single/first provider
}

type ProviderConfig struct {
	Name        string   `json:"name" yaml:"name" toml:"name"`
	Provider    string   `json:"provider" yaml:"provider" toml:"provider"`
	ModelName   string   `json:"model_name" yaml:"model_name" toml:"model_name"`
	Variant     string   `json:"variant" yaml:"variant" toml:"variant"`
	Token       string   `json:"token" yaml:"token" toml:"token"`
	Host        string   `json:"host" yaml:"host" toml:"host"`
	GoodForTask TaskType `json:"good_for_task" yaml:"good_for_task" toml:"good_for_task"`
}

func (p ProviderConfig) String() string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("%s/%s", p.Provider, p.ModelName)
}

func (p ProviderConfig) toModel(ctx context.Context) (model.Model, error) {
	switch strings.ToLower(p.Provider) {
	case "openai":
		opts := []openai.Option{}
		if p.Token != "" {
			opts = append(opts, openai.WithAPIKey(p.Token))
		}
		if p.Host != "" {
			opts = append(opts, openai.WithBaseURL(p.Host))
		}
		if p.Variant != "" {
			opts = append(opts, openai.WithVariant(openai.Variant(p.Variant)))
		}
		return openai.New(p.ModelName, opts...), nil
	case "gemini":
		opts := []gemini.Option{}
		if p.Token != "" {
			opts = append(opts, gemini.WithGeminiClientConfig(&genai.ClientConfig{
				APIKey: p.Token,
			}))
		}
		return gemini.New(ctx, p.ModelName, opts...)
	case "anthropic":
		opts := []anthropic.Option{}
		if p.Token != "" {
			opts = append(opts, anthropic.WithAPIKey(p.Token))
		}
		if p.Host != "" {
			opts = append(opts, anthropic.WithBaseURL(p.Host))
		}
		return anthropic.New(p.ModelName, opts...), nil
	case "ollama":
		opts := []ollama.Option{}
		if p.Host != "" {
			opts = append(opts, ollama.WithHost(p.Host))
		}
		return ollama.New(p.ModelName, opts...), nil
	case "huggingface":
		opts := []huggingface.Option{}
		if p.Host != "" {
			opts = append(opts, huggingface.WithBaseURL(p.Host))
		}
		if p.Token != "" {
			opts = append(opts, huggingface.WithAPIKey(p.Token))
		}
		return huggingface.New(p.ModelName, opts...)
	}
	return nil, fmt.Errorf("unknown model provider: %s", p.Provider)
}

// ModelMap is a map of model names to models
type ModelMap map[string]model.Model

func (m ModelMap) GetAny() model.Model {
	for _, model := range m {
		return model
	}
	return nil
}

func (m ModelMap) Providers() []string {
	providers := []string{}
	for name := range m {
		providers = append(providers, name)
	}
	return providers
}

//go:generate go tool counterfeiter -generate

//counterfeiter:generate trpc.group/trpc-go/trpc-agent-go/model.Model
//counterfeiter:generate . ModelProvider
type ModelProvider interface {
	GetModel(ctx context.Context, taskType TaskType) (ModelMap, error)
}

type envBasedModelProvider struct {
	cfg ModelConfig
}

func (c ModelConfig) NewEnvBasedModelProvider() ModelProvider {
	return &envBasedModelProvider{
		cfg: c,
	}
}

func (e *envBasedModelProvider) GetModel(ctx context.Context, taskType TaskType) (ModelMap, error) {
	logr := logger.GetLogger(ctx).With("fn", "envBasedModelProvider.GetModel")

	eligibleProviders, usedFallback, err := e.cfg.Providers.getForTask(taskType)
	if err != nil {
		return nil, fmt.Errorf("no LLM providers configured: please set OPENAI_API_KEY, GEMINI_API_KEY, or ANTHROPIC_API_KEY environment variable: %w", err)
	}

	// Debug: Log provider selection with fallback information
	logr.Debug("provider selected for task",
		"requested_task_type", taskType,
		"selected_provider", eligibleProviders.Providers(),
		"used_fallback", usedFallback,
	)

	return eligibleProviders.toModels(ctx)
}
