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

var _ = Describe("RetryConfig Validation", func() {
	Context("when validating retry configuration", func() {
		It("should accept valid retry config", func() {
			config := mcp.RetryConfig{
				MaxRetries:     3,
				InitialBackoff: 500 * time.Millisecond,
				BackoffFactor:  2.0,
				MaxBackoff:     8 * time.Second,
			}
			Expect(config.Validate()).To(Succeed())
		})

		It("should reject max retries above 10", func() {
			config := mcp.RetryConfig{MaxRetries: 11}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max_retries must be between 0 and 10"))
		})

		It("should reject negative max retries", func() {
			config := mcp.RetryConfig{MaxRetries: -1}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max_retries must be between 0 and 10"))
		})

		It("should reject initial backoff above 30s", func() {
			config := mcp.RetryConfig{InitialBackoff: 31 * time.Second}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("initial_backoff must be between 0 and 30s"))
		})

		It("should reject backoff factor below 1.0", func() {
			config := mcp.RetryConfig{BackoffFactor: 0.5}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("backoff_factor must be between 1.0 and 10.0"))
		})

		It("should reject backoff factor above 10.0", func() {
			config := mcp.RetryConfig{BackoffFactor: 11.0}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("backoff_factor must be between 1.0 and 10.0"))
		})

		It("should reject max backoff above 5 minutes", func() {
			config := mcp.RetryConfig{
				BackoffFactor: 2.0, // Set valid value so max_backoff validation is reached
				MaxBackoff:    6 * time.Minute,
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max_backoff must be between 0 and 5m"))
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

		It("should set retry defaults when retry config is present", func() {
			config := mcp.MCPServerConfig{
				Name:      "test",
				Transport: "stdio",
				Command:   "go",
				Retry:     &mcp.RetryConfig{},
			}
			config.SetDefaults()
			Expect(config.Retry.MaxRetries).To(Equal(2))
			Expect(config.Retry.InitialBackoff).To(Equal(500 * time.Millisecond))
			Expect(config.Retry.BackoffFactor).To(Equal(2.0))
			Expect(config.Retry.MaxBackoff).To(Equal(8 * time.Second))
		})
	})
})

var _ = Describe("RetryConfig Defaults", func() {
	Context("when setting defaults", func() {
		It("should set all default values correctly", func() {
			config := mcp.RetryConfig{}
			config.SetDefaults()

			Expect(config.MaxRetries).To(Equal(2))
			Expect(config.InitialBackoff).To(Equal(500 * time.Millisecond))
			Expect(config.BackoffFactor).To(Equal(2.0))
			Expect(config.MaxBackoff).To(Equal(8 * time.Second))
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
			Expect(server.Retry).NotTo(BeNil())
			Expect(server.Retry.MaxRetries).To(Equal(5))
			Expect(server.Retry.InitialBackoff).To(Equal(1 * time.Second))
			Expect(server.Retry.BackoffFactor).To(Equal(3.0))
			Expect(server.Retry.MaxBackoff).To(Equal(16 * time.Second))
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

			// Verify defaults were applied to the empty retry config
			retry := config.Servers[0].Retry
			Expect(retry.MaxRetries).To(Equal(2))
			Expect(retry.InitialBackoff).To(Equal(500 * time.Millisecond))
			Expect(retry.BackoffFactor).To(Equal(2.0))
			Expect(retry.MaxBackoff).To(Equal(8 * time.Second))
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
			Expect(config.Servers[0].Retry).To(BeNil())
			Expect(config.Servers[0].Timeout).To(Equal(60 * time.Second))
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
						Retry: &mcp.RetryConfig{
							MaxRetries:     4,
							InitialBackoff: 1 * time.Second,
							BackoffFactor:  3.5,
							MaxBackoff:     30 * time.Second,
						},
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
			Expect(s.Retry.MaxRetries).To(Equal(4))
			Expect(s.Retry.InitialBackoff).To(Equal(1 * time.Second))
			Expect(s.Retry.BackoffFactor).To(Equal(3.5))
			Expect(s.Retry.MaxBackoff).To(Equal(30 * time.Second))
		})
	})
})

var _ = Describe("SetDefaults Before Validate Integration", func() {
	Context("when an empty retry sub-config is provided", func() {
		It("should pass validation after SetDefaults", func() {
			config := mcp.MCPServerConfig{
				Name:      "defaults-test",
				Transport: "stdio",
				Command:   "go",
				Retry:     &mcp.RetryConfig{},
			}

			// Without SetDefaults, empty RetryConfig has BackoffFactor=0
			// which fails Validate (must be >= 1.0).
			err := config.Retry.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("backoff_factor"))

			// After SetDefaults, all fields get sensible values
			config.SetDefaults()
			Expect(config.Retry.Validate()).To(Succeed())
			Expect(config.Timeout).To(Equal(60 * time.Second))
		})
	})

	Context("when custom values are already set", func() {
		It("should not override existing non-zero values", func() {
			config := mcp.MCPServerConfig{
				Name:      "custom-test",
				Transport: "stdio",
				Command:   "node",
				Timeout:   30 * time.Second,
				Retry: &mcp.RetryConfig{
					MaxRetries:     5,
					InitialBackoff: 1 * time.Second,
					BackoffFactor:  3.0,
					MaxBackoff:     16 * time.Second,
				},
			}

			config.SetDefaults()

			// Custom values must be preserved
			Expect(config.Timeout).To(Equal(30 * time.Second))
			Expect(config.Retry.MaxRetries).To(Equal(5))
			Expect(config.Retry.InitialBackoff).To(Equal(1 * time.Second))
			Expect(config.Retry.BackoffFactor).To(Equal(3.0))
			Expect(config.Retry.MaxBackoff).To(Equal(16 * time.Second))
		})
	})

	Context("when retry config is nil", func() {
		It("should not panic and should leave retry nil", func() {
			config := mcp.MCPServerConfig{
				Name:      "nil-retry-test",
				Transport: "stdio",
				Command:   "go",
			}

			Expect(func() { config.SetDefaults() }).NotTo(Panic())
			Expect(config.Retry).To(BeNil())
			Expect(config.Timeout).To(Equal(60 * time.Second))
		})
	})
})

var _ = Describe("Additional Validation Edge Cases", func() {
	Context("when validating negative initial backoff", func() {
		It("should reject negative initial backoff", func() {
			config := mcp.RetryConfig{
				InitialBackoff: -1 * time.Second,
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("initial_backoff must be between 0 and 30s"))
		})
	})

	Context("when validating negative max backoff", func() {
		It("should reject negative max backoff", func() {
			config := mcp.RetryConfig{
				BackoffFactor: 2.0,
				MaxBackoff:    -1 * time.Second,
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max_backoff must be between 0 and 5m"))
		})
	})

	Context("when validating boundary values", func() {
		It("should accept max retries at boundaries (0 and 10)", func() {
			configZero := mcp.RetryConfig{
				MaxRetries:    0,
				BackoffFactor: 1.0,
			}
			Expect(configZero.Validate()).To(Succeed())

			configTen := mcp.RetryConfig{
				MaxRetries:    10,
				BackoffFactor: 1.0,
			}
			Expect(configTen.Validate()).To(Succeed())
		})

		It("should accept initial backoff at 0 (unset/default)", func() {
			config := mcp.RetryConfig{
				InitialBackoff: 0,
				BackoffFactor:  1.0,
			}
			Expect(config.Validate()).To(Succeed())
		})

		It("should accept initial backoff at 30s boundary", func() {
			config := mcp.RetryConfig{
				InitialBackoff: 30 * time.Second,
				BackoffFactor:  1.0,
			}
			Expect(config.Validate()).To(Succeed())
		})

		It("should accept backoff factor at boundaries (1.0 and 10.0)", func() {
			configMin := mcp.RetryConfig{BackoffFactor: 1.0}
			Expect(configMin.Validate()).To(Succeed())

			configMax := mcp.RetryConfig{BackoffFactor: 10.0}
			Expect(configMax.Validate()).To(Succeed())
		})

		It("should accept max backoff at 5m boundary", func() {
			config := mcp.RetryConfig{
				BackoffFactor: 1.0,
				MaxBackoff:    5 * time.Minute,
			}
			Expect(config.Validate()).To(Succeed())
		})
	})

	Context("when validating server config with retry errors", func() {
		It("should propagate retry validation errors", func() {
			config := mcp.MCPConfig{
				Servers: []mcp.MCPServerConfig{
					{
						Name:      "test",
						Transport: "stdio",
						Command:   "go",
						Retry: &mcp.RetryConfig{
							MaxRetries:    15,
							BackoffFactor: 2.0,
						},
					},
				},
			}
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("retry config validation failed"))
			Expect(err.Error()).To(ContainSubstring("max_retries must be between 0 and 10"))
		})
	})

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
