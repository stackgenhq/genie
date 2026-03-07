package modelprovider_test

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider/modelproviderfakes"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// newFakeSP returns a FakeSecretProvider whose GetSecret resolves names from
// the supplied map. Unlisted names return "".
func newFakeSP(secrets map[string]string) *securityfakes.FakeSecretProvider {
	sp := &securityfakes.FakeSecretProvider{}
	sp.GetSecretStub = func(_ context.Context, req security.GetSecretRequest) (string, error) {
		return secrets[req.Name], nil
	}
	return sp
}

var _ = Describe("ModelProvider", func() {
	Describe("DefaultModelConfig", func() {
		var (
			originalOpenAIKey      string
			originalGeminiKey      string
			originalGoogleKey      string
			originalOpenAIModel    string
			originalGoogleModel    string
			originalAnthropicKey   string
			originalAnthropicModel string
			hasOpenAIKey           bool
			hasGeminiKey           bool
			hasGoogleKey           bool
			hasOpenAIModel         bool
			hasGoogleModel         bool
			hasAnthropicKey        bool
			hasAnthropicModel      bool
		)

		BeforeEach(func() {
			originalOpenAIKey, hasOpenAIKey = os.LookupEnv("OPENAI_API_KEY")
			originalGeminiKey, hasGeminiKey = os.LookupEnv("GEMINI_API_KEY")
			originalGoogleKey, hasGoogleKey = os.LookupEnv("GOOGLE_API_KEY")
			originalOpenAIModel, hasOpenAIModel = os.LookupEnv("OPENAI_MODEL")
			originalGoogleModel, hasGoogleModel = os.LookupEnv("GOOGLE_MODEL")
			originalAnthropicKey, hasAnthropicKey = os.LookupEnv("ANTHROPIC_API_KEY")
			originalAnthropicModel, hasAnthropicModel = os.LookupEnv("ANTHROPIC_MODEL")

			os.Unsetenv("ANTHROPIC_API_KEY")
			os.Unsetenv("ANTHROPIC_MODEL")
		})

		AfterEach(func() {
			if hasOpenAIKey {
				os.Setenv("OPENAI_API_KEY", originalOpenAIKey)
			} else {
				os.Unsetenv("OPENAI_API_KEY")
			}
			if hasGeminiKey {
				os.Setenv("GEMINI_API_KEY", originalGeminiKey)
			} else {
				os.Unsetenv("GEMINI_API_KEY")
			}
			if hasGoogleKey {
				os.Setenv("GOOGLE_API_KEY", originalGoogleKey)
			} else {
				os.Unsetenv("GOOGLE_API_KEY")
			}
			if hasOpenAIModel {
				os.Setenv("OPENAI_MODEL", originalOpenAIModel)
			} else {
				os.Unsetenv("OPENAI_MODEL")
			}
			if hasGoogleModel {
				os.Setenv("GOOGLE_MODEL", originalGoogleModel)
			} else {
				os.Unsetenv("GOOGLE_MODEL")
			}
			if hasAnthropicKey {
				os.Setenv("ANTHROPIC_API_KEY", originalAnthropicKey)
			} else {
				os.Unsetenv("ANTHROPIC_API_KEY")
			}
			if hasAnthropicModel {
				os.Setenv("ANTHROPIC_MODEL", originalAnthropicModel)
			} else {
				os.Unsetenv("ANTHROPIC_MODEL")
			}
		})

		Context("when no API keys are set", func() {
			BeforeEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("GOOGLE_API_KEY")
			})

			It("should return an error", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(BeEmpty())
			})
		})

		Context("when only OPENAI_API_KEY is set", func() {
			BeforeEach(func() {
				os.Setenv("OPENAI_API_KEY", "test-openai-key")
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("GOOGLE_API_KEY")
				os.Unsetenv("OPENAI_MODEL")
			})

			It("should return a config with OpenAI provider using default model", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gpt-5.4"))
				Expect(cfg.Providers[0].Variant).To(Equal("default"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskEfficiency))
			})
		})

		Context("when OPENAI_API_KEY and OPENAI_MODEL are set", func() {
			BeforeEach(func() {
				os.Setenv("OPENAI_API_KEY", "test-openai-key")
				os.Setenv("OPENAI_MODEL", "gpt-4-turbo")
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("GOOGLE_API_KEY")
			})

			It("should return a config with OpenAI provider using custom model", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gpt-4-turbo"))
			})
		})

		Context("when only GEMINI_API_KEY is set", func() {
			BeforeEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Setenv("GEMINI_API_KEY", "test-gemini-key")
				os.Unsetenv("GOOGLE_API_KEY")
				os.Unsetenv("GOOGLE_MODEL")
			})

			It("should return a config with Gemini provider using default model", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(HaveLen(3)) // flash + pro (tool_calling) + pro (general_task)

				Expect(cfg.Providers[0].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gemini-3-flash-preview"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskEfficiency))

				Expect(cfg.Providers[1].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[1].ModelName).NotTo(BeEmpty())
				Expect(cfg.Providers[1].GoodForTask).To(Equal(modelprovider.TaskToolCalling))

				Expect(cfg.Providers[2].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[2].GoodForTask).To(Equal(modelprovider.TaskGeneralTask))
			})
		})

		Context("when both OPENAI_API_KEY and GEMINI_API_KEY are set", func() {
			BeforeEach(func() {
				os.Setenv("OPENAI_API_KEY", "test-openai-key")
				os.Setenv("GEMINI_API_KEY", "test-gemini-key")
				os.Unsetenv("OPENAI_MODEL")
				os.Unsetenv("GOOGLE_MODEL")
			})

			It("should return a config with both providers", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(HaveLen(4)) // openai + gemini-flash + gemini-pro (tool_calling) + gemini-pro (general_task)

				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gpt-5.4"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskEfficiency))

				Expect(cfg.Providers[1].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[1].ModelName).To(Equal("gemini-3-flash-preview"))
				Expect(cfg.Providers[1].GoodForTask).To(Equal(modelprovider.TaskEfficiency))

				Expect(cfg.Providers[2].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[2].ModelName).To(Equal(modelprovider.DefaultGeminiModel))
				Expect(cfg.Providers[2].GoodForTask).To(Equal(modelprovider.TaskToolCalling))

				Expect(cfg.Providers[3].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[3].GoodForTask).To(Equal(modelprovider.TaskGeneralTask))
			})
		})

		Context("when only ANTHROPIC_API_KEY is set", func() {
			BeforeEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("GOOGLE_API_KEY")
				os.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
				os.Unsetenv("ANTHROPIC_MODEL")
			})

			AfterEach(func() {
				os.Unsetenv("ANTHROPIC_API_KEY")
			})

			It("should return a config with Anthropic provider using default model", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("anthropic"))
				Expect(cfg.Providers[0].ModelName).To(Equal(modelprovider.DefaultAnthropicModel))
				Expect(cfg.Providers[0].Variant).To(Equal("default"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskPlanning))
			})
		})

		Context("when all three API keys are set", func() {
			BeforeEach(func() {
				os.Setenv("OPENAI_API_KEY", "test-openai-key")
				os.Setenv("GEMINI_API_KEY", "test-gemini-key")
				os.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
				os.Unsetenv("OPENAI_MODEL")
				os.Unsetenv("GOOGLE_MODEL")
				os.Unsetenv("ANTHROPIC_MODEL")
			})

			AfterEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("ANTHROPIC_API_KEY")
			})

			It("should return a config with all providers", func() {
				cfg := modelprovider.DefaultModelConfig(context.Background(), security.NewEnvProvider())
				Expect(cfg.Providers).To(HaveLen(5)) // openai + gemini-flash + gemini-pro (tool_calling) + gemini-pro (general_task) + anthropic

				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskEfficiency))

				Expect(cfg.Providers[1].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[1].ModelName).To(Equal("gemini-3-flash-preview"))

				Expect(cfg.Providers[2].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[2].GoodForTask).To(Equal(modelprovider.TaskToolCalling))

				Expect(cfg.Providers[3].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[3].GoodForTask).To(Equal(modelprovider.TaskGeneralTask))

				Expect(cfg.Providers[4].Provider).To(Equal("anthropic"))
				Expect(cfg.Providers[4].GoodForTask).To(Equal(modelprovider.TaskPlanning))
			})
		})
	})

	Describe("ProviderConfig.Validate", func() {
		Context("when provider has token", func() {
			It("succeeds for openai", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "openai", ModelName: "gpt-4", Token: "sk-x"}
				Expect(p.Validate(ctx, &securityfakes.FakeSecretProvider{})).NotTo(HaveOccurred())
			})
			It("succeeds for gemini", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "gemini", ModelName: "gemini-pro", Token: "key"}
				Expect(p.Validate(ctx, &securityfakes.FakeSecretProvider{})).NotTo(HaveOccurred())
			})
		})
		Context("when provider relies on env", func() {
			It("succeeds for openai when OPENAI_API_KEY is set", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "openai", ModelName: "gpt-4"}
				sp := newFakeSP(map[string]string{"OPENAI_API_KEY": "sk-secret"})
				Expect(p.Validate(ctx, sp)).NotTo(HaveOccurred())
			})
			It("fails for openai when no token and no env key", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "openai", ModelName: "gpt-4"}
				Expect(p.Validate(ctx, &securityfakes.FakeSecretProvider{})).To(MatchError(ContainSubstring("missing API key")))
			})
		})
		Context("ollama and huggingface", func() {
			It("succeeds for ollama without credentials", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "ollama", ModelName: "llama3"}
				Expect(p.Validate(ctx, &securityfakes.FakeSecretProvider{})).NotTo(HaveOccurred())
			})
			It("succeeds for huggingface with host", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "huggingface", ModelName: "x", Host: "https://api.inference.cloud"}
				Expect(p.Validate(ctx, &securityfakes.FakeSecretProvider{})).NotTo(HaveOccurred())
			})
			It("fails for huggingface without token or host", func(ctx context.Context) {
				p := modelprovider.ProviderConfig{Provider: "huggingface", ModelName: "x"}
				Expect(p.Validate(ctx, &securityfakes.FakeSecretProvider{})).To(MatchError(ContainSubstring("missing token or host")))
			})
		})
	})

	Describe("ProviderConfig EnableTokenTailoring", func() {
		It("builds model successfully when EnableTokenTailoring is false", func(ctx context.Context) {
			falseVal := false
			cfg := &modelprovider.ModelConfig{
				Providers: modelprovider.ProviderConfigs{
					{Provider: "openai", ModelName: "gpt-4", Token: "sk-test", EnableTokenTailoring: &falseVal},
				},
			}
			err := cfg.ValidateAndFilter(ctx, &securityfakes.FakeSecretProvider{}, modelprovider.SkipEchoCheck())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Providers).To(HaveLen(1))
		})
		It("builds model successfully when EnableTokenTailoring is nil (default true)", func(ctx context.Context) {
			cfg := &modelprovider.ModelConfig{
				Providers: modelprovider.ProviderConfigs{
					{Provider: "openai", ModelName: "gpt-4", Token: "sk-test"}, // EnableTokenTailoring nil = default on
				},
			}
			err := cfg.ValidateAndFilter(ctx, &securityfakes.FakeSecretProvider{}, modelprovider.SkipEchoCheck())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Providers).To(HaveLen(1))
		})
	})

	Describe("ModelConfig.ValidateAndFilter", func() {
		It("removes invalid providers and keeps valid ones", func(ctx context.Context) {
			cfg := &modelprovider.ModelConfig{
				Providers: modelprovider.ProviderConfigs{
					{Provider: "openai", ModelName: "gpt-4"},                  // no token
					{Provider: "anthropic", ModelName: "claude", Token: "sk"}, // valid
				},
			}
			sp := &securityfakes.FakeSecretProvider{} // no OPENAI_API_KEY
			err := cfg.ValidateAndFilter(ctx, sp, modelprovider.SkipEchoCheck())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Providers).To(HaveLen(1))
			Expect(cfg.Providers[0].Provider).To(Equal("anthropic"))
		})
		It("returns error when no providers remain valid", func(ctx context.Context) {
			cfg := &modelprovider.ModelConfig{
				Providers: modelprovider.ProviderConfigs{
					{Provider: "openai", ModelName: "gpt-4"},
					{Provider: "gemini", ModelName: "gemini-pro"},
				},
			}
			sp := &securityfakes.FakeSecretProvider{}
			err := cfg.ValidateAndFilter(ctx, sp, modelprovider.SkipEchoCheck())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no valid model providers"))
			Expect(cfg.Providers).To(BeEmpty())
		})
		It("keeps all providers when all are valid", func(ctx context.Context) {
			cfg := &modelprovider.ModelConfig{
				Providers: modelprovider.ProviderConfigs{
					{Provider: "openai", ModelName: "gpt-4", Token: "sk"},
					{Provider: "ollama", ModelName: "llama3"},
				},
			}
			Expect(cfg.ValidateAndFilter(ctx, &securityfakes.FakeSecretProvider{}, modelprovider.SkipEchoCheck())).NotTo(HaveOccurred())
			Expect(cfg.Providers).To(HaveLen(2))
		})
	})

	Describe("EchoCheckModel", func() {
		It("returns nil when the model responds without error", func(ctx context.Context) {
			ch := make(chan *model.Response, 1)
			ch <- &model.Response{}
			close(ch)
			fake := &modelproviderfakes.FakeModel{}
			fake.GenerateContentReturns(ch, nil)
			Expect(modelprovider.EchoCheckModel(ctx, fake)).NotTo(HaveOccurred())
		})
		It("returns error when the response contains API error", func(ctx context.Context) {
			ch := make(chan *model.Response, 1)
			ch <- &model.Response{Error: &model.ResponseError{Message: "401 Unauthorized"}}
			close(ch)
			fake := &modelproviderfakes.FakeModel{}
			fake.GenerateContentReturns(ch, nil)
			err := modelprovider.EchoCheckModel(ctx, fake)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("echo check failed"))
			Expect(err.Error()).To(ContainSubstring("401 Unauthorized"))
		})
		It("returns error when GenerateContent fails", func(ctx context.Context) {
			fake := &modelproviderfakes.FakeModel{}
			fake.GenerateContentReturns(nil, context.DeadlineExceeded)
			err := modelprovider.EchoCheckModel(ctx, fake)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("echo check"))
			Expect(err.Error()).To(ContainSubstring("context deadline exceeded"))
		})
	})

	Describe("envBasedModelProvider", func() {
		var (
			ctx      context.Context
			provider modelprovider.ModelProvider
		)

		BeforeEach(func() {
			ctx = context.Background()
		})

		Context("when initialized with empty config", func() {
			BeforeEach(func() {
				provider = modelprovider.ModelConfig{}.NewEnvBasedModelProvider()
			})

			It("should return an error when getting a model", func() {
				m, err := provider.GetModel(ctx, modelprovider.TaskToolCalling)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no LLM providers configured"))
				Expect(m).To(BeNil())
			})
		})

		Context("when initialized with OpenAI provider", func() {
			BeforeEach(func() {
				provider = modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{Provider: "openai", ModelName: "gpt-4", Variant: "default", GoodForTask: modelprovider.TaskEfficiency},
					},
				}.NewEnvBasedModelProvider()
			})

			It("should return an OpenAI model", func() {
				m, err := provider.GetModel(ctx, modelprovider.TaskEfficiency)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
			})
		})

		Context("when initialized with unknown provider", func() {
			BeforeEach(func() {
				provider = modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{Provider: "unknown", ModelName: "some-model", Variant: "default", GoodForTask: modelprovider.TaskEfficiency},
					},
				}.NewEnvBasedModelProvider()
			})

			It("should return an error for unknown provider", func() {
				m, err := provider.GetModel(ctx, modelprovider.TaskEfficiency)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unknown model provider"))
				Expect(m).To(BeNil())
			})
		})

		Context("when initialized with multiple providers", func() {
			BeforeEach(func() {
				provider = modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{Provider: "openai", ModelName: "gpt-4", Variant: "default", GoodForTask: modelprovider.TaskEfficiency},
						{Provider: "openai", ModelName: "gpt-5", Variant: "advanced", GoodForTask: modelprovider.TaskPlanning},
					},
				}.NewEnvBasedModelProvider()
			})

			It("should return the correct model for each task type", func() {
				m, err := provider.GetModel(ctx, modelprovider.TaskEfficiency)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())

				m, err = provider.GetModel(ctx, modelprovider.TaskPlanning)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
			})

			It("should return a model for unmatched task type", func() {
				m, err := provider.GetModel(ctx, modelprovider.TaskMathematical)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
			})
		})
	})
})
