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

func (providers ProviderConfigs) getForTask(taskType TaskType) (ProviderConfig, bool, error) {
	for _, provider := range providers {
		if provider.GoodForTask == taskType {
			return provider, false, nil // exact match, no fallback
		}
	}
	if len(providers) == 0 {
		return ProviderConfig{}, false, fmt.Errorf("no providers configured")
	}
	return providers[0], true, nil // fallback to single/first provider
}

type ProviderConfig struct {
	Provider    string   `json:"provider" yaml:"provider" toml:"provider"`
	ModelName   string   `json:"model_name" yaml:"model_name" toml:"model_name"`
	Variant     string   `json:"variant" yaml:"variant" toml:"variant"`
	Token       string   `json:"token" yaml:"token" toml:"token"`
	Host        string   `json:"host" yaml:"host" toml:"host"`
	GoodForTask TaskType `json:"good_for_task" yaml:"good_for_task" toml:"good_for_task"`
}

//go:generate go tool counterfeiter -generate

//counterfeiter:generate trpc.group/trpc-go/trpc-agent-go/model.Model
//counterfeiter:generate . ModelProvider
type ModelProvider interface {
	GetModel(ctx context.Context, taskType TaskType) (model.Model, error)
}

type envBasedModelProvider struct {
	cfg ModelConfig
}

func (c ModelConfig) NewEnvBasedModelProvider() ModelProvider {
	return &envBasedModelProvider{
		cfg: c,
	}
}

func (e *envBasedModelProvider) GetModel(ctx context.Context, taskType TaskType) (model.Model, error) {
	logr := logger.GetLogger(ctx).With("fn", "envBasedModelProvider.GetModel")

	providerConfig, usedFallback, err := e.cfg.Providers.getForTask(taskType)
	if err != nil {
		return nil, fmt.Errorf("no LLM providers configured: please set OPENAI_API_KEY, GEMINI_API_KEY, or ANTHROPIC_API_KEY environment variable: %w", err)
	}

	// Debug: Log provider selection with fallback information
	logr.Debug("provider selected for task",
		"requested_task_type", taskType,
		"selected_provider", providerConfig.Provider,
		"selected_model", providerConfig.ModelName,
		"provider_good_for", providerConfig.GoodForTask,
		"used_fallback", usedFallback,
	)

	switch strings.ToLower(providerConfig.Provider) {
	case "openai":
		opts := []openai.Option{}
		if providerConfig.Token != "" {
			opts = append(opts, openai.WithAPIKey(providerConfig.Token))
		}
		if providerConfig.Host != "" {
			opts = append(opts, openai.WithBaseURL(providerConfig.Host))
		}
		if providerConfig.Variant != "" {
			opts = append(opts, openai.WithVariant(openai.Variant(providerConfig.Variant)))
		}
		return openai.New(providerConfig.ModelName, opts...), nil
	case "gemini":
		opts := []gemini.Option{}
		if providerConfig.Token != "" {
			opts = append(opts, gemini.WithGeminiClientConfig(&genai.ClientConfig{
				APIKey: providerConfig.Token,
			}))
		}
		return gemini.New(ctx, providerConfig.ModelName, opts...)
	case "anthropic":
		opts := []anthropic.Option{}
		if providerConfig.Token != "" {
			opts = append(opts, anthropic.WithAPIKey(providerConfig.Token))
		}
		if providerConfig.Host != "" {
			opts = append(opts, anthropic.WithBaseURL(providerConfig.Host))
		}
		return anthropic.New(providerConfig.ModelName, opts...), nil
	case "ollama":
		opts := []ollama.Option{}
		if providerConfig.Host != "" {
			opts = append(opts, ollama.WithHost(providerConfig.Host))
		}
		return ollama.New(providerConfig.ModelName, opts...), nil
	case "huggingface":
		opts := []huggingface.Option{}
		if providerConfig.Host != "" {
			opts = append(opts, huggingface.WithBaseURL(providerConfig.Host))
		}
		if providerConfig.Token != "" {
			opts = append(opts, huggingface.WithAPIKey(providerConfig.Token))
		}
		return huggingface.New(providerConfig.ModelName, opts...)
	}
	return nil, fmt.Errorf("unknown model provider: %s", providerConfig.Provider)
}
