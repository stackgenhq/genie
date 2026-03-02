package mcp_test

import (
	"encoding/json"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/mcp"
)

func TestMCP(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "MCP Suite")
}

var _ = Describe("MCPConfig Validation", func() {
	Context("when validating empty config", func() {
		It("should be valid", func() {
			config := mcp.MCPConfig{}
			Expect(config.Validate()).To(Succeed())
		})
	})

	Context("when validating stdio config", func() {
		It("should accept valid stdio configuration", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test-stdio",
						Transport: "stdio",
						Command:   "go",
						Args:      []string{"run", "./server.go"},
					},
				},
			}
			Expect(config.Validate()).To(Succeed())
		})

		It("should accept stdio config with env", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "github",
						Transport: "stdio",
						Command:   "npx",
						Args:      []string{"-y", "@anthropic/github-mcp-server"},
						Env:       map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "secret"},
					},
				},
			}
			Expect(config.Validate()).To(Succeed())
		})

		It("should reject stdio config without command", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test",
						Transport: "stdio",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("command is required for stdio transport"))
		})
	})

	Context("when validating HTTP config", func() {
		It("should accept valid streamable_http configuration", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test-http",
						Transport: "streamable_http",
						ServerURL: "http://localhost:3000/mcp",
					},
				},
			}
			Expect(config.Validate()).To(Succeed())
		})

		It("should reject HTTP config without server_url", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test",
						Transport: "streamable_http",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("server_url is required"))
		})
	})

	Context("when validating SSE config", func() {
		It("should accept valid sse configuration", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test-sse",
						Transport: "sse",
						ServerURL: "http://localhost:8080/sse",
					},
				},
			}
			Expect(config.Validate()).To(Succeed())
		})
	})

	Context("when validating required fields", func() {
		It("should reject config without server name", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Transport: "stdio",
						Command:   "go",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("server name is required"))
		})

		It("should reject config without transport", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:    "test",
						Command: "go",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("transport is required"))
		})

		It("should reject config with invalid transport", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test",
						Transport: "invalid",
						Command:   "go",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid transport"))
		})
	})

	Context("when validating duplicate server names", func() {
		It("should reject config with duplicate server names", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test",
						Transport: "stdio",
						Command:   "go",
					},
					{
						Name:      "test",
						Transport: "stdio",
						Command:   "node",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate server name"))
		})
	})
})

var _ = Describe("ServerConfig Defaults", func() {
	Context("when setting defaults", func() {
		It("should set default timeout to 60s", func() {
			config := mcp.MCPServerConfig{
				Name:      "test",
				Transport: "stdio",
				Command:   "go",
			}
			config.SetDefaults()
			Expect(config.Timeout).To(Equal(60 * time.Second))
		})
	})
})

var _ = Describe("JSON Deserialization", func() {
	Context("when unmarshaling MCPConfig from JSON with snake_case keys", func() {
		It("should correctly map all fields from JSONB/PostgreSQL format", func() {
			jsonData := `{
				"servers": [
					{
						"name": "github-mcp",
						"transport": "sse",
						"server_url": "http://localhost:8080/sse",
						"timeout": 30000000000,
						"include_tools": ["github_list_prs", "github_create_issue"],
						"exclude_tools": ["github_delete_repo"],
						"session_reconnect": 3,
						"retry": {
							"max_retries": 5,
							"initial_backoff": 1000000000,
							"backoff_factor": 3.0,
							"max_backoff": 16000000000
						}
					}
				]
			}`

			var config mcp.MCPConfig
			err := json.Unmarshal([]byte(jsonData), &config)
			Expect(err).NotTo(HaveOccurred())

			Expect(config.Servers).To(HaveLen(1))
			server := config.Servers[0]
			Expect(server.Name).To(Equal("github-mcp"))
			Expect(server.Transport).To(Equal("sse"))
			Expect(server.ServerURL).To(Equal("http://localhost:8080/sse"))
			Expect(server.Timeout).To(Equal(30 * time.Second))
			Expect(server.IncludeTools).To(Equal([]string{"github_list_prs", "github_create_issue"}))
			Expect(server.ExcludeTools).To(Equal([]string{"github_delete_repo"}))
			Expect(server.SessionReconnect).To(Equal(3))
		})

		It("should handle empty retry object from JSONB and pass SetDefaults+Validate", func() {
			jsonData := `{
				"servers": [
					{
						"name": "test-server",
						"transport": "stdio",
						"command": "npx",
						"args": ["-y", "@anthropic/github-mcp-server"],
						"retry": {}
					}
				]
			}`

			var config mcp.MCPConfig
			err := json.Unmarshal([]byte(jsonData), &config)
			Expect(err).NotTo(HaveOccurred())

			// Apply defaults before validation (mirrors NewClient behavior)
			for i := range config.Servers {
				config.Servers[i].SetDefaults()
			}

			Expect(config.Validate()).To(Succeed())
		})

		It("should handle stdio config with env from JSON", func() {
			jsonData := `{
				"servers": [
					{
						"name": "github",
						"transport": "stdio",
						"command": "npx",
						"args": ["-y", "@anthropic/github-mcp-server"],
						"env": {
							"GITHUB_PERSONAL_ACCESS_TOKEN": "${GH_TOKEN}"
						}
					}
				]
			}`

			var config mcp.MCPConfig
			err := json.Unmarshal([]byte(jsonData), &config)
			Expect(err).NotTo(HaveOccurred())

			server := config.Servers[0]
			Expect(server.Env).To(HaveKeyWithValue("GITHUB_PERSONAL_ACCESS_TOKEN", "${GH_TOKEN}"))
		})

		It("should handle config with headers from JSON", func() {
			jsonData := `{
				"servers": [
					{
						"name": "my-server",
						"transport": "sse",
						"server_url": "http://localhost:3000/mcp",
						"headers": {
							"Authorization": "Bearer token123",
							"X-Custom": "value"
						}
					}
				]
			}`

			var config mcp.MCPConfig
			err := json.Unmarshal([]byte(jsonData), &config)
			Expect(err).NotTo(HaveOccurred())

			server := config.Servers[0]
			Expect(server.Headers).To(HaveKeyWithValue("Authorization", "Bearer token123"))
			Expect(server.Headers).To(HaveKeyWithValue("X-Custom", "value"))
		})
	})

	Context("when unmarshaling minimal JSON configs", func() {
		It("should handle empty servers array", func() {
			jsonData := `{"servers": []}`

			var config mcp.MCPConfig
			err := json.Unmarshal([]byte(jsonData), &config)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Validate()).To(Succeed())
		})

		It("should handle JSON with no retry field", func() {
			jsonData := `{
				"servers": [
					{
						"name": "simple",
						"transport": "stdio",
						"command": "echo"
					}
				]
			}`

			var config mcp.MCPConfig
			err := json.Unmarshal([]byte(jsonData), &config)
			Expect(err).NotTo(HaveOccurred())

			for i := range config.Servers {
				config.Servers[i].SetDefaults()
			}
			Expect(config.Validate()).To(Succeed())
		})
	})

	Context("JSON round-trip", func() {
		It("should marshal and unmarshal MCPConfig without losing data", func() {
			original := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:             "roundtrip-server",
						Transport:        "sse",
						ServerURL:        "http://example.com/sse",
						Timeout:          45 * time.Second,
						Headers:          map[string]string{"X-Key": "val"},
						IncludeTools:     []string{"tool_a"},
						ExcludeTools:     []string{"tool_b"},
						SessionReconnect: 5,
					},
				},
			}

			data, err := json.Marshal(original)
			Expect(err).NotTo(HaveOccurred())

			var restored mcp.MCPConfig
			err = json.Unmarshal(data, &restored)
			Expect(err).NotTo(HaveOccurred())

			Expect(restored.Servers).To(HaveLen(1))
			s := restored.Servers[0]
			Expect(s.Name).To(Equal("roundtrip-server"))
			Expect(s.Transport).To(Equal("sse"))
			Expect(s.ServerURL).To(Equal("http://example.com/sse"))
			Expect(s.Timeout).To(Equal(45 * time.Second))
			Expect(s.Headers).To(HaveKeyWithValue("X-Key", "val"))
			Expect(s.IncludeTools).To(Equal([]string{"tool_a"}))
			Expect(s.ExcludeTools).To(Equal([]string{"tool_b"}))
			Expect(s.SessionReconnect).To(Equal(5))
		})
	})
})

var _ = Describe("Additional Validation Edge Cases", func() {

	Context("when validating multiple servers", func() {
		It("should accept multiple valid servers with unique names", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "server-1",
						Transport: "stdio",
						Command:   "go",
					},
					{
						Name:      "server-2",
						Transport: "sse",
						ServerURL: "http://localhost:8080/sse",
					},
					{
						Name:      "server-3",
						Transport: "streamable_http",
						ServerURL: "http://localhost:3000/mcp",
					},
				},
			}
			Expect(config.Validate()).To(Succeed())
		})
	})

	Context("when SSE config is missing server_url", func() {
		It("should reject sse config without server_url", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "sse-no-url",
						Transport: "sse",
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("server_url is required for sse transport"))
		})
	})
})
