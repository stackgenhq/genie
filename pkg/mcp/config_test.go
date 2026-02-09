package mcp_test

import (
	"testing"
	"time"

	"github.com/appcd-dev/genie/pkg/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
		It("should set default timeout to 10s", func() {
			config := mcp.MCPServerConfig{
				Name:      "test",
				Transport: "stdio",
				Command:   "go",
			}
			config.SetDefaults()
			Expect(config.Timeout).To(Equal(10 * time.Second))
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
