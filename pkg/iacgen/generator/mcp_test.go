package generator

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MCPPrompts", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("MCPPrompts function", func() {
		It("should return a list of server prompts", func() {
			prompts := MCPPrompts(ctx)
			Expect(prompts).To(HaveLen(1))
		})

		It("should have the correct prompt name", func() {
			prompts := MCPPrompts(ctx)
			Expect(prompts[0].Prompt.Name).To(Equal("generate_and_validate"))
		})

		It("should have the correct prompt description", func() {
			prompts := MCPPrompts(ctx)
			Expect(prompts[0].Prompt.Description).To(Equal("Generate and validate infrastructure code"))
		})

		It("should have required arguments defined", func() {
			prompts := MCPPrompts(ctx)
			Expect(prompts[0].Prompt.Arguments).To(HaveLen(2))

			// Check architecture_requirements argument
			archReqArg := prompts[0].Prompt.Arguments[0]
			Expect(archReqArg.Name).To(Equal("architecture_requirements"))
			Expect(archReqArg.Required).To(BeTrue())
			Expect(archReqArg.Description).To(Equal("Description of the infrastructure to generate"))

			// Check output_path argument
			outputPathArg := prompts[0].Prompt.Arguments[1]
			Expect(outputPathArg.Name).To(Equal("output_path"))
			Expect(outputPathArg.Required).To(BeTrue())
			Expect(outputPathArg.Description).To(Equal("Absolute path where the code should be generated"))
		})

		It("should have a handler function", func() {
			prompts := MCPPrompts(ctx)
			Expect(prompts[0].Handler).ToNot(BeNil())
		})
	})

	Describe("Prompt Handler", func() {
		var (
			prompts []server.ServerPrompt
			handler server.PromptHandlerFunc
		)

		BeforeEach(func() {
			prompts = MCPPrompts(ctx)
			handler = prompts[0].Handler
		})

		Context("when all required arguments are provided", func() {
			It("should return a valid prompt result", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application with database",
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should have the correct description", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Description).To(Equal("Generate and Validate Infrastructure"))
			})

			It("should have exactly one message", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Messages).To(HaveLen(1))
			})

			It("should have a message with role User", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Messages[0].Role).To(Equal(mcp.RoleUser))
			})

			It("should include the architecture requirements in the message content", func() {
				requirements := "Deploy a web application with PostgreSQL database"
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": requirements,
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				textContent, ok := mcp.AsTextContent(result.Messages[0].Content)
				Expect(ok).To(BeTrue())
				Expect(textContent.Type).To(Equal("text"))
				Expect(textContent.Text).To(ContainSubstring(requirements))
			})

			It("should include the output path in the message content", func() {
				outputPath := "/tmp/my-infrastructure"
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               outputPath,
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				textContent, ok := mcp.AsTextContent(result.Messages[0].Content)
				Expect(ok).To(BeTrue())
				Expect(textContent.Text).To(ContainSubstring(outputPath))
			})

			It("should include instructions for generate_iac in the message content", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				textContent, ok := mcp.AsTextContent(result.Messages[0].Content)
				Expect(ok).To(BeTrue())
				Expect(textContent.Text).To(ContainSubstring("generate_iac"))
			})

			It("should include instructions for validate_iac in the message content", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				textContent, ok := mcp.AsTextContent(result.Messages[0].Content)
				Expect(ok).To(BeTrue())
				Expect(textContent.Text).To(ContainSubstring("validate_iac"))
			})
		})

		Context("when architecture_requirements is missing", func() {
			It("should return an error", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"output_path": "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("architecture_requirements is required"))
				Expect(result).To(BeNil())
			})
		})

		Context("when output_path is missing", func() {
			It("should return an error", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("output_path is required"))
				Expect(result).To(BeNil())
			})
		})

		Context("when both required arguments are missing", func() {
			It("should return an error for architecture_requirements first", func() {
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("architecture_requirements is required"))
				Expect(result).To(BeNil())
			})
		})

		Context("with special characters in arguments", func() {
			It("should handle special characters in requirements", func() {
				requirements := "Deploy with `special` characters & symbols <>"
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": requirements,
							"output_path":               "/tmp/infrastructure",
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				textContent, ok := mcp.AsTextContent(result.Messages[0].Content)
				Expect(ok).To(BeTrue())
				Expect(textContent.Text).To(ContainSubstring(requirements))
			})

			It("should handle paths with spaces", func() {
				outputPath := "/tmp/my infrastructure/output"
				request := mcp.GetPromptRequest{
					Params: mcp.GetPromptParams{
						Arguments: map[string]string{
							"architecture_requirements": "Deploy a web application",
							"output_path":               outputPath,
						},
					},
				}

				result, err := handler(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				textContent, ok := mcp.AsTextContent(result.Messages[0].Content)
				Expect(ok).To(BeTrue())
				Expect(textContent.Text).To(ContainSubstring(outputPath))
			})
		})
	})
})
