package halguard_test

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider/modelproviderfakes"
	"github.com/stackgenhq/genie/pkg/halguard"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// --- Test Helpers ---

// fakeModelResponse creates a model.Response channel that returns the given text.
func fakeModelResponse(text string) <-chan *model.Response {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{Content: text}},
		},
	}
	close(ch)
	return ch
}

// fakeModelError creates a model.Response channel that returns an error.
func fakeModelError(msg string) <-chan *model.Response {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Error: &model.ResponseError{Message: msg},
	}
	close(ch)
	return ch
}

// setupFakeModelProvider returns a FakeModelProvider that returns a single fake model.
func setupFakeModelProvider(responseText string) (*modelproviderfakes.FakeModelProvider, *modelproviderfakes.FakeModel) {
	fakeModel := &modelproviderfakes.FakeModel{}
	fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
		return fakeModelResponse(responseText), nil
	}

	fakeProvider := &modelproviderfakes.FakeModelProvider{}
	fakeProvider.GetModelStub = func(_ context.Context, _ modelprovider.TaskType) (modelprovider.ModelMap, error) {
		return modelprovider.ModelMap{"fake/test-model": fakeModel}, nil
	}

	return fakeProvider, fakeModel
}

// directTextGenerator creates a halguard.TextGeneratorFunc that calls
// model.GenerateContent directly. This bypasses the expert/runner pipeline
// but is suitable for unit tests where Langfuse tracing is not needed.
func directTextGenerator() halguard.TextGeneratorFunc {
	return func(ctx context.Context, m model.Model, prompt string) (string, error) {
		req := &model.Request{
			Messages: []model.Message{model.NewUserMessage(prompt)},
			GenerationConfig: model.GenerationConfig{
				Stream: true,
			},
		}

		ch, err := m.GenerateContent(ctx, req)
		if err != nil {
			return "", fmt.Errorf("generate content: %w", err)
		}

		var sb strings.Builder
		for resp := range ch {
			if resp.Error != nil {
				if sb.Len() > 0 {
					break
				}
				return "", fmt.Errorf("generation error: %s", resp.Error.Message)
			}
			for _, c := range resp.Choices {
				if c.Message.Content != "" {
					sb.WriteString(c.Message.Content)
				}
			}
		}
		return sb.String(), nil
	}
}

var _ = Describe("Guard", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("New", func() {
		It("should create a guard with default config", func() {
			fakeProvider := &modelproviderfakes.FakeModelProvider{}
			g := halguard.New(fakeProvider, directTextGenerator())
			Expect(g).NotTo(BeNil())
		})

		It("should apply functional options", func() {
			fakeProvider := &modelproviderfakes.FakeModelProvider{}
			g := halguard.New(fakeProvider, directTextGenerator(),
				halguard.WithLightThreshold(100),
				halguard.WithFullThreshold(300),
				halguard.WithCrossModelSamples(5),
				halguard.WithPreCheck(false),
				halguard.WithPostCheck(false),
			)
			Expect(g).NotTo(BeNil())
		})
	})

	Describe("PreCheck", func() {
		var (
			fakeProvider *modelproviderfakes.FakeModelProvider
			g            halguard.Guard
		)

		BeforeEach(func() {
			fakeProvider = &modelproviderfakes.FakeModelProvider{}
			g = halguard.New(fakeProvider, directTextGenerator())
		})

		Context("with pre-check disabled", func() {
			BeforeEach(func() {
				g = halguard.New(fakeProvider, directTextGenerator(), halguard.WithPreCheck(false))
			})

			It("should return high confidence when pre-check is disabled", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "you are an SRE responding to an incident",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(Equal(1.0))
				Expect(result.Summary).To(Equal("pre-check disabled"))
			})
		})

		Context("with genuine goals", func() {
			It("should return high confidence for a file search goal", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal:      "Search for all Go files containing the function NewCreateAgentTool",
					ToolNames: []string{"grep_search", "read_file"},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically(">=", 0.8))
			})

			It("should return high confidence for a code change goal", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal:      "Add a new field 'timeout' to the Config struct in pkg/halguard/options.go",
					ToolNames: []string{"read_file", "save_file"},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically(">=", 0.8))
			})

			It("should return high confidence for a deployment task", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal:      "Run terraform plan for the staging environment",
					ToolNames: []string{"run_shell"},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically(">=", 0.8))
			})

			It("should return high confidence for a simple lookup", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal:      "What is the current version of Go used in go.mod?",
					ToolNames: []string{"read_file"},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically(">=", 0.8))
			})
		})

		Context("with fabricated scenarios", func() {
			It("should return low confidence for explicit role-play", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "You are an SRE engineer. Respond to a production incident where the payment service latency spiked from 50ms to 2000ms.",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically("<", 0.5))
				Expect(result.Signals.RolePlay).To(BeNumerically(">", 0))
			})

			It("should return low confidence for hypothetical scenario", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "Imagine you're a DevOps engineer handling a critical outage. The error rate jumped from 0.1% to 15% in the last 30 minutes.",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically("<", 0.4))
			})

			It("should return low confidence for mock incident drill", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "Simulate a tabletop exercise for a database outage where users are reporting 500 errors.",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically("<", 0.6))
			})

			It("should return low confidence for role assignment with fabricated data", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "You're an incident commander. The payment-service has been down for 45 minutes. CPU usage spiked to 98%. P99 latency increased from 100ms to 5000ms.",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Confidence).To(BeNumerically("<", 0.3))
				Expect(result.Signals.RolePlay).To(BeNumerically(">", 0))
				Expect(result.Signals.FabricationPattern).To(BeNumerically(">", 0))
			})
		})

		Context("with context field containing fabrication", func() {
			It("should not penalize context because it may be summarized past messages", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal:    "Investigate the issue",
					Context: "The dashboard shows p99 latency spiked from 200ms to 3000ms in the last 15 minutes.",
				})
				Expect(err).NotTo(HaveOccurred())
				// The main goal is fine, so confidence should remain extremely high.
				Expect(result.Confidence).To(BeNumerically("==", 1.0))
			})
		})

		Context("with moderate signals", func() {
			It("should return moderate confidence for ambiguous temporal references", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "Check what happened to the payment service in the last 30 minutes",
				})
				Expect(err).NotTo(HaveOccurred())
				// This has some fabrication patterns but no role-play, so moderate.
				Expect(result.Confidence).To(BeNumerically(">=", 0.4))
				Expect(result.Confidence).To(BeNumerically("<", 1.0))
			})
		})

		Context("signal composition", func() {
			It("should include signals map with contributions", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "You are an SRE. The service has been down for 2 hours. CPU usage spiked to 100%.",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Signals.HasAny()).To(BeTrue())
				// All signal values should be between 0 and the weight max
				penalty := result.Signals.Penalty()
				Expect(penalty).To(BeNumerically(">", 0))
				Expect(penalty).To(BeNumerically("<=", 1.0))
			})

			It("should include a human-readable summary", func() {
				result, err := g.PreCheck(ctx, halguard.PreCheckRequest{
					Goal: "You are an SRE responding to an alert",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Summary).NotTo(BeEmpty())
			})
		})
	})

	Describe("PostCheck", func() {
		Context("with post-check disabled", func() {
			It("should return factual with original output", func() {
				fakeProvider := &modelproviderfakes.FakeModelProvider{}
				g := halguard.New(fakeProvider, directTextGenerator(), halguard.WithPostCheck(false))

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "search for files",
					Output:        "Found 3 files matching the pattern.",
					ToolCallsMade: 2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeTrue())
				Expect(result.CorrectedText).To(Equal("Found 3 files matching the pattern."))
				Expect(result.Tier).To(Equal(halguard.TierNone))
			})
		})

		Context("tier selection", func() {
			It("should skip verification for short tool-grounded output", func() {
				fakeProvider := &modelproviderfakes.FakeModelProvider{}
				g := halguard.New(fakeProvider, directTextGenerator())

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "check file contents",
					Output:        "File contains 42 lines.",
					ToolCallsMade: 3,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Tier).To(Equal(halguard.TierNone))
				Expect(result.IsFactual).To(BeTrue())
			})

			It("should use light tier for medium-length output with tool calls", func() {
				fakeProvider, _ := setupFakeModelProvider(`{"is_factual": true, "reason": "output is grounded"}`)
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(50),
					halguard.WithFullThreshold(500),
				)

				output := "The file config.toml contains several sections including [server], [database], and [logging]. " +
					"The server section has host=localhost and port=8080. The database section uses PostgreSQL with max_connections=100."
				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "read the config file",
					Output:        output,
					ToolCallsMade: 2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Tier).To(Equal(halguard.TierLight))
			})

			It("should use full tier for zero tool-call output exceeding light threshold", func() {
				fakeProvider, fakeModel := setupFakeModelProvider("")
				// First call: cross-model samples. Second call: judge.
				callCount := 0
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					callCount++
					if callCount <= 3 {
						return fakeModelResponse("This is a sample response for verification."), nil
					}
					return fakeModelResponse(`[{"block": 1, "label": "ACCURATE", "reason": "consistent"}]`), nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(50),
					halguard.WithFullThreshold(500),
				)

				longOutput := "This is a detailed analysis of the system architecture. " +
					"The microservices communicate via gRPC and REST APIs. " +
					"The main database is PostgreSQL with read replicas."
				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "describe the architecture",
					Output:          longOutput,
					ToolCallsMade:   0,
					GenerationModel: modelprovider.ModelMap{"anthropic/claude-sonnet-4-6": fakeModel},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Tier).To(Equal(halguard.TierFull))
			})
		})

		Context("light verification", func() {
			It("should detect factual output", func() {
				fakeProvider, _ := setupFakeModelProvider(`{"is_factual": true, "reason": "output is grounded in tool results"}`)
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(5000),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "check database status",
					Output:        "The database is running normally with 15 active connections.",
					ToolCallsMade: 2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeTrue())
				Expect(result.Tier).To(Equal(halguard.TierLight))
			})

			It("should detect fabricated output", func() {
				fakeProvider, _ := setupFakeModelProvider(`{"is_factual": false, "reason": "contains invented incident details"}`)
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(5000),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "check service status",
					Output:        "The service experienced a major outage affecting 50000 users.",
					ToolCallsMade: 1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeFalse())
				Expect(result.BlockScores).To(HaveLen(1))
				Expect(result.BlockScores[0].Label).To(Equal(halguard.BlockContradiction))
			})

			It("should handle unparseable LLM response gracefully", func() {
				fakeProvider, _ := setupFakeModelProvider("I'm not sure about that.")
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(5000),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "check something",
					Output:        "Some medium length output that triggers light verification.",
					ToolCallsMade: 1,
				})
				Expect(err).NotTo(HaveOccurred())
				// Graceful fallback: treat as factual when parsing fails
				Expect(result.IsFactual).To(BeTrue())
			})

			It("should handle model generation failure gracefully", func() {
				fakeModel := &modelproviderfakes.FakeModel{}
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					return nil, fmt.Errorf("model unavailable")
				}
				fakeProvider := &modelproviderfakes.FakeModelProvider{}
				fakeProvider.GetModelStub = func(_ context.Context, _ modelprovider.TaskType) (modelprovider.ModelMap, error) {
					return modelprovider.ModelMap{"fake/model": fakeModel}, nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(5000),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "check something",
					Output:        "Medium length output for light verification testing.",
					ToolCallsMade: 1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeTrue()) // Graceful fallback
			})
		})

		Context("model collection failure", func() {
			It("should fall back gracefully when no models available", func() {
				fakeProvider := &modelproviderfakes.FakeModelProvider{}
				fakeProvider.GetModelStub = func(_ context.Context, _ modelprovider.TaskType) (modelprovider.ModelMap, error) {
					return nil, fmt.Errorf("no models available")
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(5000),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:          "analyze data",
					Output:        "Some analysis results that need verification.",
					ToolCallsMade: 1,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeTrue()) // Graceful fallback
			})
		})

		Context("full verification", func() {
			It("should detect no contradictions", func() {
				fakeProvider, fakeModel := setupFakeModelProvider("")
				callCount := 0
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					callCount++
					if callCount <= 3 {
						// Cross-model samples
						return fakeModelResponse("The Go module uses Go 1.22. It has 15 dependencies."), nil
					}
					// Batch judge response
					return fakeModelResponse(`[{"block": 1, "label": "ACCURATE", "reason": "consistent with references"}]`), nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(20),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "check Go version",
					Output:          "The Go module uses Go 1.22.",
					ToolCallsMade:   0,
					GenerationModel: modelprovider.ModelMap{"anthropic/claude-sonnet-4-6": fakeModel},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeTrue())
				Expect(result.Tier).To(Equal(halguard.TierFull))
			})

			It("should detect contradictions and apply targeted corrections", func() {
				fakeModel1 := &modelproviderfakes.FakeModel{}
				fakeModel2 := &modelproviderfakes.FakeModel{}
				model1CallCount := 0
				model2CallCount := 0

				fakeModel1.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					model1CallCount++
					return fakeModelResponse("Go 1.22 is the version used. The module has 15 dependencies. The build system uses Make."), nil
				}
				fakeModel2.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					model2CallCount++
					return fakeModelResponse("Go 1.22 is the version used. The module has 15 dependencies. The build system uses Make."), nil
				}

				fakeProvider := &modelproviderfakes.FakeModelProvider{}
				// Return different models for different task types
				callIdx := 0
				fakeProvider.GetModelStub = func(_ context.Context, tt modelprovider.TaskType) (modelprovider.ModelMap, error) {
					callIdx++
					if callIdx%2 == 0 {
						return modelprovider.ModelMap{"anthropic/claude": fakeModel2}, nil
					}
					return modelprovider.ModelMap{"openai/gpt-4": fakeModel1}, nil
				}

				var correctorPrompt string

				// For this test, override model1 to be the judge + corrector by overriding after samples
				fakeModel1.GenerateContentStub = func(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
					model1CallCount++
					if model1CallCount == 1 {
						// Cross-model sample from model1
						return fakeModelResponse("Go 1.22 is used. 15 deps. Uses Make."), nil
					}
					if model1CallCount == 2 {
						// Batch judge
						judgeJSON := `[{"block": 1, "label": "ACCURATE", "reason": "ok"},` +
							`{"block": 2, "label": "CONTRADICTION", "reason": "references say 15 deps not 50"},` +
							`{"block": 3, "label": "ACCURATE", "reason": "ok"}]`
						return fakeModelResponse(judgeJSON), nil
					}
					// Correction call
					if len(req.Messages) > 0 {
						correctorPrompt = req.Messages[0].Content
					}
					return fakeModelResponse("The module has 15 dependencies."), nil
				}

				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(20),
				)

				output := "The module uses Go 1.22.\n\n" +
					"The module has 50 dependencies.\n\n" +
					"The build system uses Make."

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "analyze the Go module",
					Context:         "A context string to verify it is passed through",
					Output:          output,
					ToolCallsMade:   0,
					GenerationModel: modelprovider.ModelMap{"anthropic/claude-sonnet-4-6": fakeModel1},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.IsFactual).To(BeFalse())
				Expect(result.Tier).To(Equal(halguard.TierFull))
				Expect(result.CorrectedText).To(ContainSubstring("15 dependencies"))
				contradictions := 0
				for _, bs := range result.BlockScores {
					if bs.Label == halguard.BlockContradiction {
						contradictions++
					}
				}
				Expect(contradictions).To(BeNumerically(">", 0))

				// Assert that the explicit formatting of multi-model samples is actually sent to the corrector
				Expect(correctorPrompt).To(ContainSubstring("[A]"))
				Expect(correctorPrompt).To(ContainSubstring("[B]"))
				// Assert string replacement preserves formatting
				Expect(result.CorrectedText).To(Equal("The module uses Go 1.22.\n\nThe module has 15 dependencies.\n\nThe build system uses Make."))
			})

			It("should use full tier for outputs with fabrication signals when no tools used", func() {
				// An output that contains fabrication-pattern signals should trigger full tier
				// even if it's not very long, provided NO tools were used.
				fakeProvider, fakeModel := setupFakeModelProvider("")
				callCount := 0
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					callCount++
					if callCount <= 3 {
						return fakeModelResponse("Reference data here."), nil
					}
					return fakeModelResponse(`[{"block": 1, "label": "ACCURATE", "reason": "ok"}]`), nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(50),
					halguard.WithFullThreshold(500),
				)

				// Output with fabrication signals (users are reporting, spiked)
				output := "Users are reporting errors. CPU usage spiked to 95%. " +
					"The error rate jumped from 0.1% to 15%."
				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "check system status",
					Output:          output,
					ToolCallsMade:   0,
					GenerationModel: modelprovider.ModelMap{"openai/gpt-4": fakeModel},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Tier).To(Equal(halguard.TierFull))
			})

			It("should handle model error during cross-model sampling", func() {
				fakeModel := &modelproviderfakes.FakeModel{}
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					return fakeModelError("model overloaded"), nil
				}
				fakeProvider := &modelproviderfakes.FakeModelProvider{}
				fakeProvider.GetModelStub = func(_ context.Context, _ modelprovider.TaskType) (modelprovider.ModelMap, error) {
					return modelprovider.ModelMap{"fake/model": fakeModel}, nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(20),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "analyze code",
					Output:          "This is some code analysis output.",
					ToolCallsMade:   0,
					GenerationModel: modelprovider.ModelMap{"anthropic/claude-sonnet-4-6": fakeModel},
				})
				Expect(err).NotTo(HaveOccurred())
				// Should gracefully fall back when all samples fail
				Expect(result.IsFactual).To(BeTrue())
			})

			It("should handle batch judge parse failure gracefully", func() {
				fakeProvider, fakeModel := setupFakeModelProvider("")
				callCount := 0
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					callCount++
					if callCount <= 3 {
						return fakeModelResponse("Reference sample text."), nil
					}
					// Return invalid JSON for judge
					return fakeModelResponse("This is not valid JSON at all"), nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(20),
				)

				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "check something",
					Output:          "Some output to verify.",
					ToolCallsMade:   0,
					GenerationModel: modelprovider.ModelMap{"openai/gpt-4": fakeModel},
				})
				Expect(err).NotTo(HaveOccurred())
				// Should gracefully handle parse failure
				Expect(result.IsFactual).To(BeTrue())
				Expect(result.Tier).To(Equal(halguard.TierFull))
			})

			It("should select light tier for long output with tool calls", func() {
				fakeProvider, fakeModel := setupFakeModelProvider("")
				fakeModel.GenerateContentStub = func(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
					return fakeModelResponse(`{"is_factual": true, "reason": "output is grounded in tool results"}`), nil
				}
				g := halguard.New(fakeProvider, directTextGenerator(),
					halguard.WithLightThreshold(10),
					halguard.WithFullThreshold(30),
				)

				// Long output that exceeds full threshold, but has tool calls
				longOutput := "First section of content that is long. " +
					"Second section of content. Third section of content."
				result, err := g.PostCheck(ctx, halguard.PostCheckRequest{
					Goal:            "generate report",
					Output:          longOutput,
					ToolCallsMade:   5,
					GenerationModel: modelprovider.ModelMap{"google/gemini-2.0-flash": fakeModel},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Tier).To(Equal(halguard.TierLight)) // now limited to TierLight instead of TierFull
			})
		})
	})
})
