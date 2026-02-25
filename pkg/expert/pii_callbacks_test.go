package expert_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/pii"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = BeforeSuite(func() {
	// Use a deterministic salt so redaction output is stable across runs.
	cfg := pii.Config{
		Salt:             "test-stable-salt-1234567890",
		EntropyThreshold: 3.6,
		MinSecretLength:  6,
	}
	cfg.Apply()
})

var _ = AfterSuite(func() {
	// Restore defaults for other test suites.
	pii.DefaultConfig().Apply()
})

var _ = Describe("NewPIIModelCallbacks", func() {
	var (
		callbacks *model.Callbacks
		ctx       context.Context
	)

	BeforeEach(func() {
		callbacks = expert.NewPIIModelCallbacks()
		ctx = context.Background()
	})

	Describe("BeforeModel", func() {
		It("should redact PII in user messages", func() {
			req := &model.Request{
				Messages: []model.Message{
					model.NewUserMessage("my password=SuperS3cret!Val"),
				},
			}

			result, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// The content should have been modified (PII redacted).
			Expect(req.Messages[0].Content).To(ContainSubstring("[HIDDEN:"))
			Expect(req.Messages[0].Content).NotTo(ContainSubstring("SuperS3cret"))
		})

		It("should not modify system messages", func() {
			systemContent := "You are a helpful assistant. password=TopSecret123!"
			req := &model.Request{
				Messages: []model.Message{
					model.NewSystemMessage(systemContent),
				},
			}

			_, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			// System messages should be untouched.
			Expect(req.Messages[0].Content).To(Equal(systemContent))
		})

		It("should not modify assistant messages", func() {
			assistantContent := "Here is your token: sk-abc123xyz"
			req := &model.Request{
				Messages: []model.Message{
					model.NewAssistantMessage(assistantContent),
				},
			}

			_, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(req.Messages[0].Content).To(Equal(assistantContent))
		})

		It("should redact PII in tool-result messages (e.g. email_read output)", func() {
			// Tool results are appended with role "tool"; they were previously
			// not redacted, so mail/API content containing secrets reached the LLM.
			toolContent := "From: user@example.com\nSubject: Login\nBody: api_key=sk-abc123def456"
			req := &model.Request{
				Messages: []model.Message{
					{
						Role:    model.Role("tool"),
						Content: toolContent,
					},
				},
			}

			result, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			Expect(req.Messages[0].Content).To(ContainSubstring("[HIDDEN:"))
			Expect(req.Messages[0].Content).NotTo(ContainSubstring("sk-abc123def456"))
		})

		It("should skip empty user messages", func() {
			req := &model.Request{
				Messages: []model.Message{
					model.NewUserMessage(""),
				},
			}

			_, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(req.Messages[0].Content).To(Equal(""))
		})

		It("should leave clean text unchanged", func() {
			cleanText := "Please summarize this document for me."
			req := &model.Request{
				Messages: []model.Message{
					model.NewUserMessage(cleanText),
				},
			}

			_, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(req.Messages[0].Content).To(Equal(cleanText))
		})
	})

	Describe("AfterModel (rehydration)", func() {
		It("should rehydrate redacted content in responses", func() {
			secretMessage := "my api_key=xK9mP2nQ5rT8wZ3vLp"
			req := &model.Request{
				Messages: []model.Message{
					model.NewUserMessage(secretMessage),
				},
			}

			// Step 1: BeforeModel redacts the message.
			result, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Context).NotTo(BeNil())

			redactedContent := req.Messages[0].Content
			Expect(redactedContent).NotTo(Equal(secretMessage))

			// Step 2: Simulate LLM response that echoes the redacted content.
			rsp := &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Content: "I see you provided: " + redactedContent}},
				},
			}

			// Step 3: AfterModel should rehydrate the original value.
			_, err = callbacks.RunAfterModel(result.Context, &model.AfterModelArgs{
				Request:  req,
				Response: rsp,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rsp.Choices[0].Message.Content).To(ContainSubstring(secretMessage))
		})

		It("should handle nil response gracefully", func() {
			_, err := callbacks.RunAfterModel(ctx, &model.AfterModelArgs{
				Request:  &model.Request{},
				Response: nil,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not modify response when no PII was redacted", func() {
			cleanMessage := "Tell me about Go programming."
			req := &model.Request{
				Messages: []model.Message{
					model.NewUserMessage(cleanMessage),
				},
			}

			result, err := callbacks.RunBeforeModel(ctx, &model.BeforeModelArgs{
				Request: req,
			})
			Expect(err).NotTo(HaveOccurred())

			responseContent := "Go is a statically typed language."
			rsp := &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Content: responseContent}},
				},
			}

			afterCtx := ctx
			if result != nil && result.Context != nil {
				afterCtx = result.Context
			}

			_, err = callbacks.RunAfterModel(afterCtx, &model.AfterModelArgs{
				Request:  req,
				Response: rsp,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rsp.Choices[0].Message.Content).To(Equal(responseContent))
		})
	})
})
