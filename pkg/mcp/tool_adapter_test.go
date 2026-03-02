package mcp_test

import (
	"context"
	"errors"
	"os"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/mcp/mcpfakes"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
)

var _ = Describe("ClientTool Adapter", func() {
	var (
		mockTool mcplib.Tool
		adapter  *mcp.ClientTool
	)

	BeforeEach(func() {
		mockTool = mcplib.Tool{
			Name:        "test_tool",
			Description: "A test tool for testing",
			InputSchema: mcplib.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"arg1": map[string]interface{}{
						"type":        "string",
						"description": "First argument",
					},
					"arg2": map[string]interface{}{
						"type":        "integer",
						"description": "Second argument",
					},
				},
			},
		}
		// nil client is safe here because we only test metadata, not Call()
		adapter = mcp.NewClientTool(nil, mockTool, "testserver")
	})

	Describe("Name", func() {
		It("should return the namespaced tool name", func() {
			Expect(adapter.Name()).To(Equal("testserver_test_tool"))
		})
	})

	Describe("Description", func() {
		It("should return the MCP tool description", func() {
			Expect(adapter.Description()).To(Equal("A test tool for testing"))
		})
	})

	Describe("Declaration", func() {
		It("should return a valid tool.Declaration", func() {
			decl := adapter.Declaration()
			Expect(decl).NotTo(BeNil())
			Expect(decl.Name).To(Equal("testserver_test_tool"))
			Expect(decl.Description).To(Equal("A test tool for testing"))
		})

		It("should convert the MCP input schema", func() {
			decl := adapter.Declaration()
			Expect(decl.InputSchema).NotTo(BeNil())
			Expect(decl.InputSchema.Type).To(Equal("object"))
			Expect(decl.InputSchema.Properties).To(HaveKey("arg1"))
			Expect(decl.InputSchema.Properties).To(HaveKey("arg2"))
		})
	})

	Describe("tool.Tool interface compliance", func() {
		It("should satisfy tool.Tool via Declaration()", func() {
			// This test is compile-time verified by the type assertion.
			// The adapter must implement Declaration() *tool.Declaration.
			decl := adapter.Declaration()
			Expect(decl.Name).NotTo(BeEmpty())
		})
	})

	Describe("Call", func() {
		var (
			fakeCaller *mcpfakes.FakeMCPCaller
			tool       *mcp.ClientTool
		)

		BeforeEach(func() {
			fakeCaller = &mcpfakes.FakeMCPCaller{}
			tool = mcp.NewClientTool(fakeCaller, mockTool, "testserver")
		})

		Context("with valid JSON arguments", func() {
			It("should pass parsed arguments to CallTool and return text content", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{
						mcplib.TextContent{Type: "text", Text: "hello world"},
					},
				}, nil)

				// Act
				result, err := tool.Call(ctx, []byte(`{"arg1":"value1","arg2":42}`))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("hello world\n"))

				Expect(fakeCaller.CallToolCallCount()).To(Equal(1))
				_, req := fakeCaller.CallToolArgsForCall(0)
				Expect(req.Params.Name).To(Equal("test_tool"), "should use original MCP name, not namespaced")
				Expect(req.Params.Arguments).To(HaveKeyWithValue("arg1", "value1"))
				Expect(req.Params.Arguments).To(HaveKeyWithValue("arg2", BeNumerically("==", 42)))
			})
		})

		Context("with empty arguments", func() {
			It("should pass empty map to CallTool", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{
						mcplib.TextContent{Type: "text", Text: "ok"},
					},
				}, nil)

				// Act
				result, err := tool.Call(ctx, nil)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("ok\n"))

				_, req := fakeCaller.CallToolArgsForCall(0)
				Expect(req.Params.Arguments).To(BeEmpty())
			})

			It("should also handle zero-length byte slice", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{
						mcplib.TextContent{Type: "text", Text: "ok"},
					},
				}, nil)

				// Act
				result, err := tool.Call(ctx, []byte{})

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("ok\n"))
			})
		})

		Context("with invalid JSON arguments", func() {
			It("should return a descriptive error", func(ctx context.Context) {
				// Arrange — no setup needed, invalid input is the trigger

				// Act
				result, err := tool.Call(ctx, []byte(`{not-valid-json`))

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid JSON input for tool testserver_test_tool"))
				Expect(result).To(BeNil())
				Expect(fakeCaller.CallToolCallCount()).To(Equal(0), "should not call MCP server with bad input")
			})
		})

		Context("when MCP server returns an error", func() {
			It("should propagate the error", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(nil, errors.New("connection refused"))

				// Act
				result, err := tool.Call(ctx, []byte(`{"arg1":"test"}`))

				// Assert
				Expect(err).To(MatchError("connection refused"))
				Expect(result).To(BeNil())
			})
		})

		Context("with multiple text content items", func() {
			It("should concatenate all text content", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{
						mcplib.TextContent{Type: "text", Text: "line one"},
						mcplib.TextContent{Type: "text", Text: "line two"},
						mcplib.TextContent{Type: "text", Text: "line three"},
					},
				}, nil)

				// Act
				result, err := tool.Call(ctx, []byte(`{}`))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("line one\nline two\nline three\n"))
			})
		})

		Context("with image content", func() {
			It("should format image as [Image: data]", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{
						mcplib.ImageContent{Type: "image", Data: "base64data", MIMEType: "image/png"},
					},
				}, nil)

				// Act
				result, err := tool.Call(ctx, []byte(`{}`))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(ContainSubstring("[Image: base64data]"))
			})
		})

		Context("with mixed text and image content", func() {
			It("should concatenate all content in order", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{
						mcplib.TextContent{Type: "text", Text: "description"},
						mcplib.ImageContent{Type: "image", Data: "img123", MIMEType: "image/jpeg"},
						mcplib.TextContent{Type: "text", Text: "caption"},
					},
				}, nil)

				// Act
				result, err := tool.Call(ctx, []byte(`{}`))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				resultStr := result.(string)
				Expect(resultStr).To(ContainSubstring("description"))
				Expect(resultStr).To(ContainSubstring("[Image: img123]"))
				Expect(resultStr).To(ContainSubstring("caption"))
			})
		})

		Context("with empty response content", func() {
			It("should return empty string", func(ctx context.Context) {
				// Arrange
				fakeCaller.CallToolReturns(&mcplib.CallToolResult{
					Content: []mcplib.Content{},
				}, nil)

				// Act
				result, err := tool.Call(ctx, []byte(`{}`))

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(""))
			})
		})
	})
})

var _ = Describe("Client", func() {
	Describe("shouldIncludeTool", func() {
		var client *mcp.Client

		BeforeEach(func() {
			client = mcp.NewClientForTest()
		})

		Context("with no include or exclude lists", func() {
			It("should include all tools", func() {
				// Arrange
				config := mcp.MCPServerConfig{Name: "test"}

				// Act
				included := client.ShouldIncludeToolForTest("any_tool", config)

				// Assert
				Expect(included).To(BeTrue())
			})
		})

		Context("with include list", func() {
			It("should include tools in the list", func() {
				// Arrange
				config := mcp.MCPServerConfig{
					Name:         "test",
					IncludeTools: []string{"search", "read_file"},
				}

				// Act & Assert
				Expect(client.ShouldIncludeToolForTest("search", config)).To(BeTrue())
				Expect(client.ShouldIncludeToolForTest("read_file", config)).To(BeTrue())
			})

			It("should exclude tools not in the list", func() {
				// Arrange
				config := mcp.MCPServerConfig{
					Name:         "test",
					IncludeTools: []string{"search"},
				}

				// Act
				included := client.ShouldIncludeToolForTest("delete_file", config)

				// Assert
				Expect(included).To(BeFalse())
			})
		})

		Context("with exclude list", func() {
			It("should exclude tools in the list", func() {
				// Arrange
				config := mcp.MCPServerConfig{
					Name:         "test",
					ExcludeTools: []string{"delete_file", "drop_table"},
				}

				// Act & Assert
				Expect(client.ShouldIncludeToolForTest("delete_file", config)).To(BeFalse())
				Expect(client.ShouldIncludeToolForTest("drop_table", config)).To(BeFalse())
			})

			It("should include tools not in the list", func() {
				// Arrange
				config := mcp.MCPServerConfig{
					Name:         "test",
					ExcludeTools: []string{"delete_file"},
				}

				// Act
				included := client.ShouldIncludeToolForTest("search", config)

				// Assert
				Expect(included).To(BeTrue())
			})
		})

		Context("with both include and exclude lists", func() {
			It("should only include tools in the include list and not in the exclude list", func() {
				// Arrange
				config := mcp.MCPServerConfig{
					Name:         "test",
					IncludeTools: []string{"search", "read_file", "delete_file"},
					ExcludeTools: []string{"delete_file"},
				}

				// Act & Assert
				Expect(client.ShouldIncludeToolForTest("search", config)).To(BeTrue())
				Expect(client.ShouldIncludeToolForTest("read_file", config)).To(BeTrue())
				Expect(client.ShouldIncludeToolForTest("delete_file", config)).To(BeFalse(), "exclude takes precedence")
				Expect(client.ShouldIncludeToolForTest("other", config)).To(BeFalse(), "not in include list")
			})
		})
	})

	Describe("buildStdioEnv", func() {
		Context("without secret provider", func() {
			It("should return parent env plus config overrides", func(ctx context.Context) {
				// Arrange
				client := mcp.NewClientForTest()
				config := mcp.MCPServerConfig{
					Name:      "test",
					Transport: "stdio",
					Env:       map[string]string{"MY_KEY": "my_value"},
				}

				// Act
				env := client.BuildStdioEnvForTest(ctx, config)

				// Assert
				found := false
				for _, e := range env {
					if e == "MY_KEY=my_value" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "should contain MY_KEY=my_value")
			})

			It("should include parent environment", func(ctx context.Context) {
				// Arrange
				client := mcp.NewClientForTest()
				config := mcp.MCPServerConfig{
					Name:      "test",
					Transport: "stdio",
					Env:       map[string]string{},
				}

				// Act
				env := client.BuildStdioEnvForTest(ctx, config)

				// Assert
				Expect(len(env)).To(BeNumerically(">", 0))
				hasPath := false
				for _, e := range env {
					if strings.HasPrefix(e, "PATH=") {
						hasPath = true
						break
					}
				}
				Expect(hasPath).To(BeTrue(), "should include PATH from parent env")
			})

			It("should return parent env when config has no env overrides", func(ctx context.Context) {
				// Arrange
				client := mcp.NewClientForTest()
				config := mcp.MCPServerConfig{
					Name:      "test",
					Transport: "stdio",
				}

				// Act
				env := client.BuildStdioEnvForTest(ctx, config)

				// Assert
				Expect(env).To(Equal(os.Environ()))
			})
		})

		Context("with secret provider", func() {
			It("should expand ${VAR} values via the secret provider", func(ctx context.Context) {
				// Arrange
				fakeSP := &securityfakes.FakeSecretProvider{}
				fakeSP.GetSecretCalls(func(_ context.Context, req security.GetSecretRequest) (string, error) {
					if req.Name == "GH_TOKEN" {
						return "secret-token-123", nil
					}
					return "", errors.New("unknown secret")
				})

				client := mcp.NewClientForTest(mcp.WithSecretProvider(fakeSP))
				config := mcp.MCPServerConfig{
					Name:      "github",
					Transport: "stdio",
					Env:       map[string]string{"GITHUB_TOKEN": "${GH_TOKEN}"},
				}

				// Act
				env := client.BuildStdioEnvForTest(ctx, config)

				// Assert
				found := false
				for _, e := range env {
					if e == "GITHUB_TOKEN=secret-token-123" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "should expand ${GH_TOKEN} to secret-token-123")
			})

			It("should leave values without ${} unexpanded", func(ctx context.Context) {
				// Arrange
				fakeSP := &securityfakes.FakeSecretProvider{}
				client := mcp.NewClientForTest(mcp.WithSecretProvider(fakeSP))
				config := mcp.MCPServerConfig{
					Name:      "test",
					Transport: "stdio",
					Env:       map[string]string{"PLAIN_KEY": "plain_value"},
				}

				// Act
				env := client.BuildStdioEnvForTest(ctx, config)

				// Assert
				found := false
				for _, e := range env {
					if e == "PLAIN_KEY=plain_value" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue())
				Expect(fakeSP.GetSecretCallCount()).To(Equal(0), "should not call secret provider for plain values")
			})
		})
	})

	Describe("expandEnvValue", func() {
		It("should expand ${NAME} using the secret provider", func(ctx context.Context) {
			// Arrange
			fakeSP := &securityfakes.FakeSecretProvider{}
			fakeSP.GetSecretReturns("expanded-value", nil)
			client := mcp.NewClientForTest(mcp.WithSecretProvider(fakeSP))

			// Act
			result := client.ExpandEnvValueForTest(ctx, "${MY_SECRET}")

			// Assert
			Expect(result).To(Equal("expanded-value"))
		})

		It("should handle multiple variables in a single value", func(ctx context.Context) {
			// Arrange
			fakeSP := &securityfakes.FakeSecretProvider{}
			fakeSP.GetSecretCalls(func(_ context.Context, req security.GetSecretRequest) (string, error) {
				switch req.Name {
				case "HOST":
					return "localhost", nil
				case "PORT":
					return "8080", nil
				default:
					return "", errors.New("unknown")
				}
			})
			client := mcp.NewClientForTest(mcp.WithSecretProvider(fakeSP))

			// Act
			result := client.ExpandEnvValueForTest(ctx, "http://${HOST}:${PORT}/api")

			// Assert
			Expect(result).To(Equal("http://localhost:8080/api"))
		})

		It("should return empty string on secret lookup failure", func(ctx context.Context) {
			// Arrange
			fakeSP := &securityfakes.FakeSecretProvider{}
			fakeSP.GetSecretReturns("", errors.New("not found"))
			client := mcp.NewClientForTest(mcp.WithSecretProvider(fakeSP))

			// Act
			result := client.ExpandEnvValueForTest(ctx, "${MISSING_SECRET}")

			// Assert
			Expect(result).To(Equal(""))
		})
	})

	Describe("GetTools", func() {
		It("should return empty slice when no servers are configured", func() {
			// Arrange
			client := mcp.NewClientForTest()

			// Act
			tools := client.GetTools()

			// Assert
			Expect(tools).To(BeEmpty())
		})
	})

	Describe("NewClient", func() {
		Context("with invalid configuration", func() {
			It("should return an error for missing server name", func(ctx context.Context) {
				// Arrange
				config := mcp.MCPConfig{
					Servers: []mcp.MCPServerConfig{
						{Transport: "stdio", Command: "echo"},
					},
				}

				// Act
				client, err := mcp.NewClient(ctx, config)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid MCP configuration"))
				Expect(client).To(BeNil())
			})

			It("should return an error for unsupported transport", func(ctx context.Context) {
				// Arrange
				config := mcp.MCPConfig{
					Servers: []mcp.MCPServerConfig{
						{Name: "test", Transport: "grpc", Command: "echo"},
					},
				}

				// Act
				client, err := mcp.NewClient(ctx, config)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid MCP configuration"))
				Expect(client).To(BeNil())
			})
		})

		Context("with empty configuration", func() {
			It("should create a client with no tools", func(ctx context.Context) {
				// Arrange
				config := mcp.MCPConfig{}

				// Act
				client, err := mcp.NewClient(ctx, config)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(client).NotTo(BeNil())
				Expect(client.GetTools()).To(BeEmpty())
				client.Close(ctx)
			})
		})
	})
})
