package secops

import (
	"context"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SecOpsConfig", func() {
	Describe("DefaultSecOpsConfig", func() {
		It("should return default configuration with correct thresholds", func() {
			cfg := DefaultSecOpsConfig()
			Expect(cfg.SeverityThresholds.High).To(Equal(1))
			Expect(cfg.SeverityThresholds.Medium).To(Equal(10))
			Expect(cfg.SeverityThresholds.Low).To(Equal(-1))
		})

		It("should use snyk as default scanner", func() {
			cfg := DefaultSecOpsConfig()
			Expect(cfg.Scanner).To(Equal("snyk"))
		})

		It("should respect GENIE_SECOPS_SCANNER environment variable", func() {
			os.Setenv("GENIE_SECOPS_SCANNER", "trivy")
			defer os.Unsetenv("GENIE_SECOPS_SCANNER")

			cfg := DefaultSecOpsConfig()
			Expect(cfg.Scanner).To(Equal("trivy"))
		})
	})

	Describe("Tool", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should return TrivyPolicyChecker when scanner is trivy", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			tool, err := cfg.Tool(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tool).NotTo(BeNil())
			_, ok := tool.(TrivyPolicyChecker)
			Expect(ok).To(BeTrue())
		})

		It("should return TrivyPolicyChecker when scanner is TRIVY (case insensitive)", func() {
			cfg := SecOpsConfig{
				Scanner: "TRIVY",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			tool, err := cfg.Tool(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, ok := tool.(TrivyPolicyChecker)
			Expect(ok).To(BeTrue())
		})

		It("should return snykPolicyChecker when scanner is snyk", func() {
			cfg := SecOpsConfig{
				Scanner: "snyk",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			tool, err := cfg.Tool(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(tool).NotTo(BeNil())
			_, ok := tool.(snykPolicyChecker)
			Expect(ok).To(BeTrue())
		})

		It("should default to snykPolicyChecker for unknown scanner", func() {
			cfg := SecOpsConfig{
				Scanner: "unknown-scanner",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			tool, err := cfg.Tool(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, ok := tool.(snykPolicyChecker)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("MCPTool", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should create MCP tool with correct name and description", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			serverTool, err := cfg.MCPTool(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(serverTool.Tool.Name).To(Equal("check_iac_policy"))
			Expect(serverTool.Tool.Description).To(ContainSubstring("infrastructure-as-code"))
		})

		It("should have iac_path as required parameter", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			serverTool, err := cfg.MCPTool(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(serverTool.Tool.InputSchema.Properties).To(HaveKey("iac_path"))
		})

		It("should handle tool execution with valid path", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   -1,
					Medium: -1,
					Low:    -1,
				},
			}
			serverTool, err := cfg.MCPTool(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Create a temp directory for testing
			tmpDir, err := os.MkdirTemp("", "mcp-test")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "check_iac_policy",
					Arguments: map[string]interface{}{
						"iac_path": tmpDir,
					},
				},
			}

			result, err := serverTool.Handler(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Content).To(HaveLen(1))
		})

		It("should return error when iac_path is missing", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			serverTool, err := cfg.MCPTool(ctx)
			Expect(err).NotTo(HaveOccurred())

			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "check_iac_policy",
					Arguments: map[string]interface{}{},
				},
			}

			_, err = serverTool.Handler(ctx, request)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("MCPPrompts", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should return review_infrastructure_security prompt", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			prompts := cfg.MCPPrompts(ctx)
			Expect(prompts).To(HaveLen(1))
			Expect(prompts[0].Prompt.Name).To(Equal("review_infrastructure_security"))
		})

		It("should have iac_path as required argument", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			prompts := cfg.MCPPrompts(ctx)
			Expect(prompts[0].Prompt.Arguments).To(HaveLen(1))
			Expect(prompts[0].Prompt.Arguments[0].Name).To(Equal("iac_path"))
			Expect(prompts[0].Prompt.Arguments[0].Required).To(BeTrue())
		})

		It("should execute prompt handler with valid iac_path", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			prompts := cfg.MCPPrompts(ctx)

			request := mcp.GetPromptRequest{
				Params: mcp.GetPromptParams{
					Name: "review_infrastructure_security",
					Arguments: map[string]string{
						"iac_path": "/path/to/terraform",
					},
				},
			}

			result, err := prompts[0].Handler(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Description).To(Equal("Review Infrastructure Security"))
			Expect(result.Messages).To(HaveLen(1))
			Expect(result.Messages[0].Role).To(Equal(mcp.RoleUser))
		})

		It("should return error when iac_path is missing", func() {
			cfg := SecOpsConfig{
				Scanner: "trivy",
				SeverityThresholds: SeverityThresholds{
					High:   1,
					Medium: 10,
					Low:    -1,
				},
			}
			prompts := cfg.MCPPrompts(ctx)

			request := mcp.GetPromptRequest{
				Params: mcp.GetPromptParams{
					Name:      "review_infrastructure_security",
					Arguments: map[string]string{},
				},
			}

			_, err := prompts[0].Handler(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("iac_path is required"))
		})
	})
})
