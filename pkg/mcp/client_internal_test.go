// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// client_internal_test.go contains tests that require package-internal access.
// Black-box tests for shouldIncludeTool, buildStdioEnv, expandEnvValue, GetTools,
// NewClient, and Close live in tool_adapter_test.go using exported test helpers.

var _ = Describe("Client Internal Tests", func() {
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

			tools, err := c.convertAndFilterTools(context.Background(), mcpTools, config)
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

			tools, err := c.convertAndFilterTools(context.Background(), mcpTools, config)
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

			tools, err := c.convertAndFilterTools(context.Background(), mcpTools, config)
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

			tools, err := c.convertAndFilterTools(context.Background(), mcpTools, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeEmpty())
		})

		It("should handle empty tool list", func() {
			c := &Client{}
			config := MCPServerConfig{Name: "srv"}
			tools, err := c.convertAndFilterTools(context.Background(), []mcplib.Tool{}, config)
			Expect(err).NotTo(HaveOccurred())
			Expect(tools).To(BeEmpty())
		})
	})
})
