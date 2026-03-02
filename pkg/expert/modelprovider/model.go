package modelprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpc.group/trpc-go/trpc-agent-go/model/gemini"
	"trpc.group/trpc-go/trpc-agent-go/model/huggingface"
	"trpc.group/trpc-go/trpc-agent-go/model/ollama"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

const (
	DefaultGeminiModel      = "gemini-3-flash-preview"
	DefaultAnthropicModel   = "claude-sonnet-4-6"
	DefaultOpenAIModel      = "gpt-4o"
	DefaultOllamaModel      = "llama3"
	DefaultHuggingFaceModel = "bert-base-uncased"
)

type ModelConfig struct {
	Providers ProviderConfigs `json:"providers" yaml:"providers,omitempty" toml:"providers,omitempty"`
}

// echoCheckTimeout is the maximum time allowed for each provider's echo check at startup.
const echoCheckTimeout = 15 * time.Second

// ValidateAndFilterOption configures ValidateAndFilter behavior.
type ValidateAndFilterOption func(*validateAndFilterOptions)

type validateAndFilterOptions struct {
	skipEchoCheck bool
}

// SkipEchoCheck disables the per-provider echo check (useful in tests to avoid real API calls).
func SkipEchoCheck() ValidateAndFilterOption {
	return func(o *validateAndFilterOptions) {
		o.skipEchoCheck = true
	}
}

// ValidateAndFilter keeps only providers that pass Validate and (unless skipped) EchoCheckModel, and mutates c.Providers.
// Providers that fail credential validation or the echo check (token invalid/401) are excluded with a warning.
// Returns an error if after filtering no providers remain.
func (c *ModelConfig) ValidateAndFilter(ctx context.Context, sp security.SecretProvider, opts ...ValidateAndFilterOption) error {
	opt := &validateAndFilterOptions{}
	for _, f := range opts {
		f(opt)
	}
	logr := logger.GetLogger(ctx).With("fn", "ModelConfig.ValidateAndFilter")
	valid := make(ProviderConfigs, 0, len(c.Providers))
	for _, p := range c.Providers {
		if err := p.Validate(ctx, sp); err != nil {
			logr.Warn("excluding invalid model provider", "provider", p.Provider, "model", p.ModelName, "reason", err)
			continue
		}
		mod, err := p.toModel(ctx)
		if err != nil {
			logr.Warn("excluding model provider (failed to build model)", "provider", p.Provider, "model", p.ModelName, "reason", err)
			continue
		}
		if !opt.skipEchoCheck {
			echoCtx, cancel := context.WithTimeout(ctx, echoCheckTimeout)
			err = EchoCheckModel(echoCtx, mod)
			cancel()
			if err != nil {
				logr.Warn("excluding model provider (echo check failed — token invalid or unreachable)", "provider", p.Provider, "model", p.ModelName, "reason", err)
				continue
			}
		}
		valid = append(valid, p)
	}
	c.Providers = valid
	if len(c.Providers) == 0 {
		// Fall back to env-derived defaults so that e.g. GEMINI_API_KEY alone works
		// even when the config file lists only a different provider that failed (e.g. Anthropic).
		defaultCfg := DefaultModelConfig(ctx, sp)
		if len(defaultCfg.Providers) > 0 {
			logr.Info("no configured providers passed validation; trying env-based defaults (OPENAI_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY, ANTHROPIC_API_KEY)")
			get := func(name string) string {
				v, _ := sp.GetSecret(ctx, security.GetSecretRequest{
					Name:   name,
					Reason: toolcontext.GetJustification(ctx),
				})
				return v
			}
			for _, p := range defaultCfg.Providers {
				// Resolve token from secret provider so toModel can pass it to the client.
				p = resolveTokenForDefaultProvider(p, get)
				if err := p.Validate(ctx, sp); err != nil {
					logr.Warn("excluding default provider", "provider", p.Provider, "model", p.ModelName, "reason", err)
					continue
				}
				mod, err := p.toModel(ctx)
				if err != nil {
					logr.Warn("excluding default provider (failed to build)", "provider", p.Provider, "model", p.ModelName, "reason", err)
					continue
				}
				if !opt.skipEchoCheck {
					echoCtx, cancel := context.WithTimeout(ctx, echoCheckTimeout)
					err = EchoCheckModel(echoCtx, mod)
					cancel()
					if err != nil {
						logr.Warn("excluding default provider (echo check failed)", "provider", p.Provider, "model", p.ModelName, "reason", err)
						continue
					}
				}
				valid = append(valid, p)
			}
			c.Providers = valid
		}
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("no valid model providers: ensure at least one of OPENAI_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY, or ANTHROPIC_API_KEY is set and exported (e.g. export OPENAI_API_KEY) so the genie process can see it, or configure valid providers with tokens in config")
	}
	return nil
}

// resolveTokenForDefaultProvider sets Token on a default provider from the given
// get(name) secret lookup so that toModel can pass the key to the client.
func resolveTokenForDefaultProvider(p ProviderConfig, get func(string) string) ProviderConfig {
	switch strings.ToLower(p.Provider) {
	case "openai":
		if v := get("OPENAI_API_KEY"); v != "" {
			p.Token = v
		}
	case "gemini":
		if v := get("GEMINI_API_KEY"); v != "" {
			p.Token = v
		} else if v := get("GOOGLE_API_KEY"); v != "" {
			p.Token = v
		}
	case "anthropic":
		if v := get("ANTHROPIC_API_KEY"); v != "" {
			p.Token = v
		}
	}
	return p
}

// DefaultModelConfig builds the default model configuration by resolving
// API keys through the given SecretProvider. Each provider is added only
// if its API key is present. Without a SecretProvider, callers can pass
// security.NewEnvProvider() to preserve the legacy os.Getenv behavior.
func DefaultModelConfig(ctx context.Context, sp security.SecretProvider) ModelConfig {
	// Helper to resolve a secret, ignoring errors (treat as empty).
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, security.GetSecretRequest{
			Name:   name,
			Reason: toolcontext.GetJustification(ctx),
		})
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
			ModelName:   DefaultGeminiModel,
			GoodForTask: TaskEfficiency,
		})
		result.Providers = append(result.Providers, ProviderConfig{
			Provider:    "gemini",
			ModelName:   getWithDefault("GOOGLE_MODEL", DefaultGeminiModel),
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
			ModelName:   getWithDefault("ANTHROPIC_MODEL", DefaultAnthropicModel),
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
	Name        string   `json:"name" yaml:"name,omitempty" toml:"name,omitempty"`
	Provider    string   `json:"provider" yaml:"provider,omitempty" toml:"provider,omitempty"`
	ModelName   string   `json:"model_name" yaml:"model_name,omitempty" toml:"model_name,omitempty"`
	Variant     string   `json:"variant" yaml:"variant,omitempty" toml:"variant,omitempty"`
	Token       string   `json:"token" yaml:"token,omitempty" toml:"token,omitempty"`
	Host        string   `json:"host" yaml:"host,omitempty" toml:"host,omitempty"`
	GoodForTask TaskType `json:"good_for_task" yaml:"good_for_task,omitempty" toml:"good_for_task,omitempty"`
	// EnableTokenTailoring when true (default) trims conversation history to the model's context window (arXiv:2601.14192).
	// Set to false to disable (e.g. debugging or when the provider handles context itself).
	EnableTokenTailoring *bool `json:"enable_token_tailoring,omitempty" yaml:"enable_token_tailoring,omitempty" toml:"enable_token_tailoring,omitempty"`
}

func (p ProviderConfig) String() string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("%s/%s", p.Provider, p.ModelName)
}

// Validate returns an error if this provider is not usable (e.g. missing API key).
// It uses the given SecretProvider to resolve env-based keys when Token is empty.
// Call this before using the provider so the server never starts with invalid credentials.
func (p ProviderConfig) Validate(ctx context.Context, sp security.SecretProvider) error {
	get := func(name string) string {
		v, _ := sp.GetSecret(ctx, security.GetSecretRequest{
			Name:   name,
			Reason: toolcontext.GetJustification(ctx),
		})
		return v
	}
	switch strings.ToLower(p.Provider) {
	case "openai":
		if p.Token != "" {
			return nil
		}
		if get("OPENAI_API_KEY") != "" {
			return nil
		}
		return fmt.Errorf("openai provider %q: missing API key (set token or OPENAI_API_KEY)", p.ModelName)
	case "gemini":
		if p.Token != "" {
			return nil
		}
		if get("GEMINI_API_KEY") != "" || get("GOOGLE_API_KEY") != "" {
			return nil
		}
		return fmt.Errorf("gemini provider %q: missing API key (set token or GEMINI_API_KEY/GOOGLE_API_KEY)", p.ModelName)
	case "anthropic":
		if p.Token != "" {
			return nil
		}
		if get("ANTHROPIC_API_KEY") != "" {
			return nil
		}
		return fmt.Errorf("anthropic provider %q: missing API key (set token or ANTHROPIC_API_KEY)", p.ModelName)
	case "ollama":
		return nil
	case "huggingface":
		if p.Token != "" || p.Host != "" {
			return nil
		}
		return fmt.Errorf("huggingface provider %q: missing token or host", p.ModelName)
	default:
		return fmt.Errorf("unknown provider %q", p.Provider)
	}
}

// enableTokenTailoring returns true when token tailoring should be enabled (default when unset).
func (p ProviderConfig) enableTokenTailoring() bool {
	if p.EnableTokenTailoring == nil {
		return true
	}
	return *p.EnableTokenTailoring
}

// anthropicModelMaxOutput maps known Anthropic model names to their hard API
// max_tokens limits. These limits are separate from the input context window:
// e.g. claude-sonnet-4-6 has a 200k input window but only 128k max output tokens.
// Token tailoring computes output tokens from the context window remainder, which
// can exceed the API limit and cause a 400. This map is used to cap the value.
// Return 0 for unknown models (no cap applied — tailoring computes freely).
var anthropicModelMaxOutput = map[string]int{
	// Claude 4.x confirmed from API error: max_tokens: 179487 > 128000
	"claude-sonnet-4-6": 128000,
	"claude-opus-4-6":   128000,
	// Claude 3.7
	"claude-3-7-sonnet": 64000,
	// Claude 3.5
	"claude-3-5-sonnet": 8192,
	"claude-3-5-haiku":  8192,
	// Claude 3
	"claude-3-opus":   4096,
	"claude-3-sonnet": 4096,
	"claude-3-haiku":  4096,
}

// resolveAnthropicMaxOutput returns the API max output token limit for a model.
// Exact name match first, then prefix-based fallback, then 0 (no cap).
func resolveAnthropicMaxOutput(modelName string) int {
	key := strings.ToLower(modelName)
	if v, ok := anthropicModelMaxOutput[key]; ok {
		return v
	}
	for k, v := range anthropicModelMaxOutput {
		if strings.HasPrefix(key, k) {
			return v
		}
	}
	return 0
}

// maxOutputCapModel wraps model.Model and caps GenerationConfig.MaxTokens before
// each GenerateContent call. This ensures token tailoring never sends a max_tokens
// value that exceeds the model's hard API output limit.
// The underlying model's input token tailoring (message trimming) still runs normally
// because that part of applyTokenTailoring is independent of MaxTokens.
type maxOutputCapModel struct {
	inner     model.Model
	maxOutput int
}

func (m *maxOutputCapModel) Info() model.Info {
	return m.inner.Info()
}

func (m *maxOutputCapModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	if req != nil && m.maxOutput > 0 {
		if req.MaxTokens == nil || *req.MaxTokens > m.maxOutput {
			capped := m.maxOutput
			req.MaxTokens = &capped
		}
	}
	return m.inner.GenerateContent(ctx, req)
}

func (p ProviderConfig) toModel(ctx context.Context) (model.Model, error) {
	tailoring := p.enableTokenTailoring()
	if tailoring {
		logger.GetLogger(ctx).Debug("token tailoring enabled for model provider",
			"provider", p.Provider, "model", p.ModelName)
	}
	switch strings.ToLower(p.Provider) {
	case "openai":
		opts := []openai.Option{}
		if tailoring {
			opts = append(opts, openai.WithEnableTokenTailoring(true))
		}
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
		if tailoring {
			opts = append(opts, gemini.WithEnableTokenTailoring(true))
		}
		if p.Token != "" {
			opts = append(opts, gemini.WithGeminiClientConfig(&genai.ClientConfig{
				APIKey: p.Token,
			}))
		}
		return gemini.New(ctx, p.ModelName, opts...)
	case "anthropic":
		opts := []anthropic.Option{}
		if tailoring {
			opts = append(opts, anthropic.WithEnableTokenTailoring(true))
		}
		if p.Token != "" {
			opts = append(opts, anthropic.WithAPIKey(p.Token))
		}
		if p.Host != "" {
			opts = append(opts, anthropic.WithBaseURL(p.Host))
		}
		mod := anthropic.New(p.ModelName, opts...)
		if maxOut := resolveAnthropicMaxOutput(p.ModelName); maxOut > 0 {
			return &maxOutputCapModel{inner: mod, maxOutput: maxOut}, nil
		}
		return mod, nil
	case "ollama":
		opts := []ollama.Option{}
		if tailoring {
			opts = append(opts, ollama.WithEnableTokenTailoring(true))
		}
		if p.Host != "" {
			opts = append(opts, ollama.WithHost(p.Host))
		}
		return ollama.New(p.ModelName, opts...), nil
	case "huggingface":
		opts := []huggingface.Option{}
		if tailoring {
			opts = append(opts, huggingface.WithEnableTokenTailoring(true))
		}
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

// echoCheckPrompt is a minimal user message used by EchoCheckModel to verify
// that the model accepts the request and the token is valid (no 401/403).
const echoCheckPrompt = "Hi"

// echoCheckMaxTokens is the max_tokens value sent during the echo check.
// Setting it explicitly prevents token tailoring from computing a value that
// exceeds the model's API output limit (e.g. 179k > 128k for claude-sonnet-4-6).
const echoCheckMaxTokens = 10

// EchoCheckModel performs a minimal GenerateContent call (echo check) to verify
// that the given model's credentials are valid. It returns nil if the model
// responds successfully, and an error if the API returns an auth error (e.g. 401)
// or a system-level failure. Use this to validate tokens before using a provider.
func EchoCheckModel(ctx context.Context, m model.Model) error {
	maxTok := echoCheckMaxTokens
	req := &model.Request{
		Messages: []model.Message{model.NewUserMessage(echoCheckPrompt)},
		GenerationConfig: model.GenerationConfig{
			Stream:    true,
			MaxTokens: &maxTok,
		},
	}
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return fmt.Errorf("echo check: %w", err)
	}
	for r := range ch {
		if r.Error != nil {
			return fmt.Errorf("echo check failed: %s", r.Error.Message)
		}
	}
	return nil
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
