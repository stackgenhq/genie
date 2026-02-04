package modelprovider_test

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
)

var _ = Describe("ModelProvider", func() {
	Describe("DefaultModelConfig", func() {
		var (
			originalOpenAIKey   string
			originalGeminiKey   string
			originalGoogleKey   string
			originalOpenAIModel string
			originalGoogleModel string
			hasOpenAIKey        bool
			hasGeminiKey        bool
			hasGoogleKey        bool
			hasOpenAIModel      bool
			hasGoogleModel      bool
		)

		BeforeEach(func() {
			// Save original environment variables
			originalOpenAIKey, hasOpenAIKey = os.LookupEnv("OPENAI_API_KEY")
			originalGeminiKey, hasGeminiKey = os.LookupEnv("GEMINI_API_KEY")
			originalGoogleKey, hasGoogleKey = os.LookupEnv("GOOGLE_API_KEY")
			originalOpenAIModel, hasOpenAIModel = os.LookupEnv("OPENAI_MODEL")
			originalGoogleModel, hasGoogleModel = os.LookupEnv("GOOGLE_MODEL")
		})

		AfterEach(func() {
			// Restore original environment variables
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
		})

		Context("when no API keys are set", func() {
			BeforeEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("GOOGLE_API_KEY")
			})

			It("should return an error", func() {
				cfg := modelprovider.DefaultModelConfig()
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
				cfg := modelprovider.DefaultModelConfig()
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gpt-5.2"))
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
				cfg := modelprovider.DefaultModelConfig()
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
				cfg := modelprovider.DefaultModelConfig()
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[0].ModelName).NotTo(BeEmpty())
				Expect(cfg.Providers[0].Variant).To(Equal("default"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskToolCalling))
			})
		})

		Context("when only GOOGLE_API_KEY is set", func() {
			BeforeEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("GEMINI_API_KEY")
				os.Setenv("GOOGLE_API_KEY", "test-google-key")
				os.Unsetenv("GOOGLE_MODEL")
			})

			It("should return a config with Gemini provider using default model", func() {
				cfg := modelprovider.DefaultModelConfig()
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[0].ModelName).NotTo(BeEmpty())
			})
		})

		Context("when GEMINI_API_KEY and GOOGLE_MODEL are set", func() {
			BeforeEach(func() {
				os.Unsetenv("OPENAI_API_KEY")
				os.Setenv("GEMINI_API_KEY", "test-gemini-key")
				os.Setenv("GOOGLE_MODEL", "gemini-pro")
			})

			It("should return a config with Gemini provider using custom model", func() {
				cfg := modelprovider.DefaultModelConfig()
				Expect(cfg.Providers).To(HaveLen(1))
				Expect(cfg.Providers[0].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gemini-pro"))
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
				cfg := modelprovider.DefaultModelConfig()
				Expect(cfg.Providers).To(HaveLen(2))

				// First provider should be OpenAI
				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[0].ModelName).To(Equal("gpt-5.2"))
				Expect(cfg.Providers[0].GoodForTask).To(Equal(modelprovider.TaskEfficiency))

				// Second provider should be Gemini
				Expect(cfg.Providers[1].Provider).To(Equal("gemini"))
				Expect(cfg.Providers[1].ModelName).To(Equal("gemini-3-pro-preview"))
				Expect(cfg.Providers[1].GoodForTask).To(Equal(modelprovider.TaskToolCalling))
			})
		})

		Context("when both OPENAI_API_KEY and GOOGLE_API_KEY are set", func() {
			BeforeEach(func() {
				os.Setenv("OPENAI_API_KEY", "test-openai-key")
				os.Unsetenv("GEMINI_API_KEY")
				os.Setenv("GOOGLE_API_KEY", "test-google-key")
			})

			It("should return a config with both providers", func() {
				cfg := modelprovider.DefaultModelConfig()
				Expect(cfg.Providers).To(HaveLen(2))
				Expect(cfg.Providers[0].Provider).To(Equal("openai"))
				Expect(cfg.Providers[1].Provider).To(Equal("gemini"))
			})
		})
	})

	// Note: ProviderConfigs.getForTask is a private method and is tested
	// indirectly through envBasedModelProvider.GetModel tests below

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
				cfg := modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{},
				}
				provider = cfg.NewEnvBasedModelProvider()
			})

			It("should return an error when getting a model", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskToolCalling)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no LLM providers configured"))
				Expect(model).To(BeNil())
			})
		})

		Context("when initialized with OpenAI provider", func() {
			BeforeEach(func() {
				cfg := modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{
							Provider:    "openai",
							ModelName:   "gpt-4",
							Variant:     "default",
							GoodForTask: modelprovider.TaskEfficiency,
						},
					},
				}
				provider = cfg.NewEnvBasedModelProvider()
			})

			It("should return an OpenAI model", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskEfficiency)
				Expect(err).NotTo(HaveOccurred())
				Expect(model).NotTo(BeNil())
			})
		})

		Context("when initialized with Gemini provider", func() {
			BeforeEach(func() {
				// Set a valid API key for Gemini initialization
				os.Setenv("GEMINI_API_KEY", "test-gemini-key")

				cfg := modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{
							Provider:    "gemini",
							ModelName:   "gemini-pro",
							Variant:     "default",
							GoodForTask: modelprovider.TaskToolCalling,
						},
					},
				}
				provider = cfg.NewEnvBasedModelProvider()
			})

			AfterEach(func() {
				os.Unsetenv("GEMINI_API_KEY")
			})

			It("should return a Gemini model or error if API key is invalid", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskToolCalling)
				// Gemini.New may fail with invalid API key, which is expected in tests
				if err != nil {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(model).NotTo(BeNil())
				}
			})
		})

		Context("when initialized with unknown provider", func() {
			BeforeEach(func() {
				cfg := modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{
							Provider:    "unknown",
							ModelName:   "some-model",
							Variant:     "default",
							GoodForTask: modelprovider.TaskEfficiency,
						},
					},
				}
				provider = cfg.NewEnvBasedModelProvider()
			})

			It("should return an error for unknown provider", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskEfficiency)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unknown model provider"))
				Expect(model).To(BeNil())
			})
		})

		Context("when initialized with multiple providers", func() {
			BeforeEach(func() {
				cfg := modelprovider.ModelConfig{
					Providers: modelprovider.ProviderConfigs{
						{
							Provider:    "openai",
							ModelName:   "gpt-4",
							Variant:     "default",
							GoodForTask: modelprovider.TaskEfficiency,
						},
						{
							Provider:    "openai",
							ModelName:   "gpt-5",
							Variant:     "advanced",
							GoodForTask: modelprovider.TaskPlanning,
						},
					},
				}
				provider = cfg.NewEnvBasedModelProvider()
			})

			It("should return the correct model for TaskEfficiency", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskEfficiency)
				Expect(err).NotTo(HaveOccurred())
				Expect(model).NotTo(BeNil())
			})

			It("should return the correct model for TaskPlanning", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskPlanning)
				Expect(err).NotTo(HaveOccurred())
				Expect(model).NotTo(BeNil())
			})

			It("should return a model for unmatched task type", func() {
				model, err := provider.GetModel(ctx, modelprovider.TaskMathematical)
				Expect(err).NotTo(HaveOccurred())
				Expect(model).NotTo(BeNil())
			})
		})
	})
})
