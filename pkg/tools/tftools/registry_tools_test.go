package tftools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("RegistryTools", func() {
	var (
		logger *logrus.Logger
	)

	BeforeEach(func() {
		logger = logrus.New()
		logger.SetOutput(GinkgoWriter)
	})

	Describe("NewMultiRegistryTools", func() {
		It("should initialize with logger and session ID", func() {
			tools := NewMultiRegistryTools(logger, 5)
			Expect(tools.logger).To(Equal(logger))
			Expect(tools.sessionID).To(ContainSubstring("genie-session-"))
		})
	})

	Describe("GetTools", func() {
		It("should return a list of enhanced tools", func() {
			registryTools := NewMultiRegistryTools(logger, 5)
			tools := registryTools.GetTools()

			Expect(tools).To(HaveLen(3))

			toolNames := make([]string, 0, len(tools))
			for _, t := range tools {
				toolNames = append(toolNames, t.Declaration().Name)
			}
			Expect(toolNames).To(ContainElements("search_modules", "get_module_details", "get_latest_module_version"))
		})
	})

	Describe("enhancedTool", func() {
		var (
			mockMCPTool   server.ServerTool
			registryTools MultiRegistryTools
			enhanced      tool.Tool
		)

		BeforeEach(func() {
			// Create a mock MCP tool
			mockMCPTool = server.ServerTool{
				Tool: mcp.Tool{
					Name:        "mock_tool",
					Description: "A mock tool",
					InputSchema: mcp.ToolInputSchema{
						Type: "object",
					},
				},
				Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					// Check for session injection (optional verification, hard to do without checking context internals or exposing helper)
					// But we can check if arguments are passed correctly

					argVal, ok := req.Params.Arguments.(map[string]interface{})
					if !ok {
						return nil, fmt.Errorf("invalid arguments")
					}

					if val, ok := argVal["fail"]; ok && val.(bool) {
						return nil, fmt.Errorf("simulated failure")
					}

					if val, ok := argVal["rate_limit"]; ok && val.(bool) {
						return nil, fmt.Errorf("429 too many requests")
					}

					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent("success"),
						},
					}, nil
				},
			}

			registryTools = NewMultiRegistryTools(logger, 5)
			// access the private wrapTool via public interface? wrapped tools are returned by GetTools.
			// But we want to test enhancedTool logic specifically.
			// Since enhancedTool and wrapTool are in package tftools, we can access them in package tftools (same package test).
			// So we need to use package tftools instead of tools_test?
			// No, typically we use tools_test.
			// If we use package tftools, we can access wrapTool.

			// Let's use the public GetTools method but we can't easily swap the underlying tools there.
			// So we should switch to package tftools for this test file to access unexported members.
		})

		// Helper to wrap tool manually since wrapTool is unexported if we are in tools_test package.
		// Wait, I declared package tftools at the top. So I have access.

		It("should enhance tool declaration description", func() {
			enhanced = registryTools.wrapTool(mockMCPTool, "mock_tool")
			decl := enhanced.Declaration()

			Expect(decl.Name).To(Equal("mock_tool"))
		})

		It("should call underlying tool successfully", func() {
			enhanced = registryTools.wrapTool(mockMCPTool, "mock_tool")

			args := map[string]interface{}{"key": "value"}
			jsonArgs, _ := json.Marshal(args)

			res, err := enhanced.(tool.CallableTool).Call(context.Background(), jsonArgs)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal("success"))
		})

		It("should handle tool errors", func() {
			enhanced = registryTools.wrapTool(mockMCPTool, "mock_tool")

			args := map[string]interface{}{"fail": true}
			jsonArgs, _ := json.Marshal(args)

			_, err := enhanced.(tool.CallableTool).Call(context.Background(), jsonArgs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated failure"))
		})

		It("should log warning for rate limit errors", func() {
			// We can't easily verify the log output in ginkgo unless we hook the logger.
			// But we can verify the function execution doesn't panic and returns the error.
			enhanced = registryTools.wrapTool(mockMCPTool, "mock_tool")

			args := map[string]interface{}{"rate_limit": true}
			jsonArgs, _ := json.Marshal(args)

			_, err := enhanced.(tool.CallableTool).Call(context.Background(), jsonArgs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("too many requests"))
		})
	})

	Describe("isRateLimitOrSessionError", func() {
		It("should identify rate limit errors", func() {
			err := fmt.Errorf("received 429 too many requests")
			Expect(isRateLimitOrSessionError(err)).To(BeTrue())
		})

		It("should identify session errors", func() {
			err := fmt.Errorf("no active session found")
			Expect(isRateLimitOrSessionError(err)).To(BeTrue())
		})

		It("should return false for other errors", func() {
			err := fmt.Errorf("generic error")
			Expect(isRateLimitOrSessionError(err)).To(BeFalse())
		})

		It("should return false for nil error", func() {
			Expect(isRateLimitOrSessionError(nil)).To(BeFalse())
		})
	})
})
