package mcp

import (
	"context"
	"fmt"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// newFakeSecretProvider creates a FakeSecretProvider backed by a map of secret
// name → value. GetSecret calls return the matching value or an error when the
// name is not present in the map.
func newFakeSecretProvider(secrets map[string]string) *securityfakes.FakeSecretProvider {
	sp := &securityfakes.FakeSecretProvider{}
	sp.GetSecretCalls(func(_ context.Context, req security.GetSecretRequest) (string, error) {
		val, ok := secrets[req.Name]
		if !ok {
			return "", fmt.Errorf("secret %q not found", req.Name)
		}
		return val, nil
	})
	return sp
}

var _ = Describe("Client Internal Tests", func() {
	Describe("shouldIncludeTool", func() {
		var c *Client

		BeforeEach(func() {
			c = &Client{}
		})

		Context("when no include or exclude lists are set", func() {
			It("should include all tools", func() {
				config := MCPServerConfig{}
				Expect(c.shouldIncludeTool("any_tool", config)).To(BeTrue())
				Expect(c.shouldIncludeTool("another_tool", config)).To(BeTrue())
			})
		})

		Context("when include list is set", func() {
			It("should only include tools in the list", func() {
				config := MCPServerConfig{
					IncludeTools: []string{"tool_a", "tool_b"},
				}
				Expect(c.shouldIncludeTool("tool_a", config)).To(BeTrue())
				Expect(c.shouldIncludeTool("tool_b", config)).To(BeTrue())
				Expect(c.shouldIncludeTool("tool_c", config)).To(BeFalse())
			})
		})

		Context("when exclude list is set", func() {
			It("should exclude tools in the list", func() {
				config := MCPServerConfig{
					ExcludeTools: []string{"tool_x", "tool_y"},
				}
				Expect(c.shouldIncludeTool("tool_a", config)).To(BeTrue())
				Expect(c.shouldIncludeTool("tool_x", config)).To(BeFalse())
				Expect(c.shouldIncludeTool("tool_y", config)).To(BeFalse())
			})
		})

		Context("when both include and exclude lists are set", func() {
			It("should apply include first then exclude", func() {
				config := MCPServerConfig{
					IncludeTools: []string{"tool_a", "tool_b", "tool_c"},
					ExcludeTools: []string{"tool_b"},
				}
				Expect(c.shouldIncludeTool("tool_a", config)).To(BeTrue())
				Expect(c.shouldIncludeTool("tool_b", config)).To(BeFalse()) // in both, but excluded
				Expect(c.shouldIncludeTool("tool_c", config)).To(BeTrue())
				Expect(c.shouldIncludeTool("tool_d", config)).To(BeFalse()) // not in include list
			})
		})

		Context("when include list is empty slice", func() {
			It("should behave as no filter (include all)", func() {
				config := MCPServerConfig{
					IncludeTools: []string{},
				}
				Expect(c.shouldIncludeTool("any_tool", config)).To(BeTrue())
			})
		})

		Context("when exclude list is empty slice", func() {
			It("should not exclude anything", func() {
				config := MCPServerConfig{
					ExcludeTools: []string{},
				}
				Expect(c.shouldIncludeTool("any_tool", config)).To(BeTrue())
			})
		})
	})

	Describe("WithSecretProvider", func() {
		It("should set the secret provider on the client", func() {
			sp := &securityfakes.FakeSecretProvider{}
			c := &Client{}
			opt := WithSecretProvider(sp)
			opt(c)
			Expect(c.secretProvider).NotTo(BeNil())
		})
	})

	Describe("GetTools", func() {
		It("should return nil tools when not initialized", func() {
			c := &Client{
				tools: nil,
			}
			Expect(c.GetTools()).To(BeNil())
		})

		It("should return empty slice when initialized with empty", func() {
			c := &Client{
				tools: make([]tool.Tool, 0),
			}
			Expect(c.GetTools()).To(BeEmpty())
		})
	})

	Describe("Close", func() {
		It("should not panic with nil clients slice", func() {
			c := &Client{
				clients: nil,
			}
			Expect(func() { c.Close(context.Background()) }).NotTo(Panic())
		})

		It("should not panic with empty clients slice", func() {
			c := &Client{
				clients: make([]*mcpclient.Client, 0),
			}
			Expect(func() { c.Close(context.Background()) }).NotTo(Panic())
		})
	})

	Describe("buildStdioEnv", func() {
		var c *Client

		Context("when no env overrides are configured", func() {
			It("should return the base environment", func() {
				c = &Client{}
				config := MCPServerConfig{}
				env := c.buildStdioEnv(context.Background(), config)
				Expect(env).NotTo(BeEmpty()) // at minimum has parent env vars
			})
		})

		Context("when env overrides are configured", func() {
			It("should include the override values", func() {
				c = &Client{}
				config := MCPServerConfig{
					Env: map[string]string{
						"MY_TEST_VAR": "test_value_12345",
					},
				}
				env := c.buildStdioEnv(context.Background(), config)
				found := false
				for _, e := range env {
					if e == "MY_TEST_VAR=test_value_12345" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "env should contain MY_TEST_VAR=test_value_12345")
			})
		})

		Context("when secret provider is set and env has ${VAR} reference", func() {
			It("should expand secrets via the provider", func() {
				sp := newFakeSecretProvider(map[string]string{
					"GH_TOKEN": "ghp_secret123",
				})
				c = &Client{secretProvider: sp}
				config := MCPServerConfig{
					Env: map[string]string{
						"GITHUB_PERSONAL_ACCESS_TOKEN": "${GH_TOKEN}",
					},
				}
				env := c.buildStdioEnv(context.Background(), config)
				found := false
				for _, e := range env {
					if e == "GITHUB_PERSONAL_ACCESS_TOKEN=ghp_secret123" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "env should contain expanded secret")
			})
		})

		Context("when secret provider fails to resolve", func() {
			It("should use empty string for missing secrets", func() {
				sp := newFakeSecretProvider(map[string]string{})
				c = &Client{secretProvider: sp}
				config := MCPServerConfig{
					Env: map[string]string{
						"TOKEN": "${MISSING_SECRET}",
					},
				}
				env := c.buildStdioEnv(context.Background(), config)
				found := false
				for _, e := range env {
					if e == "TOKEN=" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "env should contain TOKEN= with empty value")
			})
		})

		Context("when env value does not contain ${ but provider is set", func() {
			It("should use the value as-is without secret expansion", func() {
				sp := newFakeSecretProvider(map[string]string{})
				c = &Client{secretProvider: sp}
				config := MCPServerConfig{
					Env: map[string]string{
						"PLAIN_VAR": "plain_value",
					},
				}
				env := c.buildStdioEnv(context.Background(), config)
				found := false
				for _, e := range env {
					if e == "PLAIN_VAR=plain_value" {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "env should contain PLAIN_VAR=plain_value")
			})
		})
	})

	Describe("expandEnvValue", func() {
		It("should expand ${VAR} using the secret provider", func() {
			sp := newFakeSecretProvider(map[string]string{"TOKEN": "secret_val"})
			c := &Client{secretProvider: sp}
			result := c.expandEnvValue(context.Background(), "Bearer ${TOKEN}")
			Expect(result).To(Equal("Bearer secret_val"))
		})

		It("should expand $VAR without braces", func() {
			sp := newFakeSecretProvider(map[string]string{"TOKEN": "abc"})
			c := &Client{secretProvider: sp}
			result := c.expandEnvValue(context.Background(), "$TOKEN")
			Expect(result).To(Equal("abc"))
		})

		It("should return empty for unresolvable secrets", func() {
			sp := newFakeSecretProvider(map[string]string{})
			c := &Client{secretProvider: sp}
			result := c.expandEnvValue(context.Background(), "${NONEXISTENT}")
			Expect(result).To(Equal(""))
		})

		It("should handle multiple variables in one value", func() {
			sp := newFakeSecretProvider(map[string]string{"USER": "admin", "PASS": "p@ss"})
			c := &Client{secretProvider: sp}
			result := c.expandEnvValue(context.Background(), "${USER}:${PASS}")
			Expect(result).To(Equal("admin:p@ss"))
		})
	})

	Describe("NewClient", func() {
		Context("when config validation fails", func() {
			It("should return error for invalid config", func() {
				config := MCPConfig{
					Servers: []MCPServerConfig{
						{
							Name:      "",
							Transport: "stdio",
						},
					},
				}
				_, err := NewClient(context.Background(), config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid MCP configuration"))
			})
		})

		Context("when config has invalid transport", func() {
			It("should return error during server initialization", func() {
				config := MCPConfig{
					Servers: []MCPServerConfig{
						{
							Name:      "test",
							Transport: "invalid_transport",
							Command:   "echo",
						},
					},
				}
				_, err := NewClient(context.Background(), config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid MCP configuration"))
			})
		})

		Context("when SetDefaults is called before Validate in NewClient", func() {
			It("should succeed for config with empty retry object", func() {
				config := MCPConfig{
					Servers: []MCPServerConfig{
						{
							Name:      "test-defaults",
							Transport: "stdio",
							Command:   "echo",
							Retry:     &RetryConfig{},
						},
					},
				}
				// NewClient calls SetDefaults before Validate, so empty
				// RetryConfig should not cause validation failure.
				// It will fail on initializeServer (no real server),
				// but NOT on validation.
				_, err := NewClient(context.Background(), config)
				if err != nil {
					// The error should be about server initialization, not validation
					Expect(err.Error()).NotTo(ContainSubstring("invalid MCP configuration"))
					Expect(err.Error()).To(ContainSubstring("failed to initialize server"))
				}
			})
		})
	})

	Describe("convertAndFilterTools", func() {
		It("should convert MCP tools to trpc-agent-go tools", func() {
			c := &Client{}
			mcpTools := []mcplib.Tool{
				{
					Name:        "list_files",
					Description: "Lists files in a directory",
					InputSchema: mcplib.ToolInputSchema{
						Type: "object",
						Properties: map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "Directory path",
							},
						},
					},
				},
				{
					Name:        "read_file",
					Description: "Reads a file",
				},
			}
			config := MCPServerConfig{Name: "fs"}

			tools, err := c.convertAndFilterTools(context.Background(), nil, mcpTools, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(HaveLen(2))
			Expect(tools[0].Declaration().Name).To(Equal("fs_list_files"))
			Expect(tools[1].Declaration().Name).To(Equal("fs_read_file"))
		})

		It("should filter tools based on include list", func() {
			c := &Client{}
			mcpTools := []mcplib.Tool{
				{Name: "tool_a", Description: "Tool A"},
				{Name: "tool_b", Description: "Tool B"},
				{Name: "tool_c", Description: "Tool C"},
			}
			config := MCPServerConfig{
				Name:         "srv",
				IncludeTools: []string{"tool_a", "tool_c"},
			}

			tools, err := c.convertAndFilterTools(context.Background(), nil, mcpTools, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(HaveLen(2))
			Expect(tools[0].Declaration().Name).To(Equal("srv_tool_a"))
			Expect(tools[1].Declaration().Name).To(Equal("srv_tool_c"))
		})

		It("should filter tools based on exclude list", func() {
			c := &Client{}
			mcpTools := []mcplib.Tool{
				{Name: "tool_a", Description: "Tool A"},
				{Name: "tool_b", Description: "Tool B"},
				{Name: "tool_c", Description: "Tool C"},
			}
			config := MCPServerConfig{
				Name:         "srv",
				ExcludeTools: []string{"tool_b"},
			}

			tools, err := c.convertAndFilterTools(context.Background(), nil, mcpTools, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(HaveLen(2))
			Expect(tools[0].Declaration().Name).To(Equal("srv_tool_a"))
			Expect(tools[1].Declaration().Name).To(Equal("srv_tool_c"))
		})

		It("should return empty tools when all are excluded", func() {
			c := &Client{}
			mcpTools := []mcplib.Tool{
				{Name: "tool_a", Description: "Tool A"},
			}
			config := MCPServerConfig{
				Name:         "srv",
				ExcludeTools: []string{"tool_a"},
			}

			tools, err := c.convertAndFilterTools(context.Background(), nil, mcpTools, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeEmpty())
		})

		It("should handle empty tool list", func() {
			c := &Client{}
			config := MCPServerConfig{Name: "srv"}
			tools, err := c.convertAndFilterTools(context.Background(), nil, []mcplib.Tool{}, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeEmpty())
		})
	})

	Describe("Close", func() {
		It("should handle nil entries in clients slice", func() {
			c := &Client{
				clients: []*mcpclient.Client{nil, nil},
			}
			Expect(func() { c.Close(context.Background()) }).NotTo(Panic())
		})
	})
})
