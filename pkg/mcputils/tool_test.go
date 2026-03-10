// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcputils_test

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/mcputils"
)

var _ = Describe("MCPTool", func() {

	Describe("NewMCPTool", func() {
		It("should create a new MCPTool with logger", func() {
			mcpTool := server.ServerTool{
				Tool: mcp.Tool{
					Name:        "test_tool",
					Description: "A test tool",
					InputSchema: mcp.ToolInputSchema{
						Type: "object",
						Properties: map[string]any{
							"param": map[string]any{
								"type": "string",
							},
						},
					},
				},
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			}

			tool := mcputils.NewMCPTool(mcpTool)
			Expect(tool).ToNot(BeNil())
		})

		It("should create a new MCPTool with nil logger", func() {
			mcpTool := server.ServerTool{
				Tool: mcp.Tool{
					Name:        "test_tool",
					Description: "A test tool",
					InputSchema: mcp.ToolInputSchema{
						Type: "object",
					},
				},
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			}

			tool := mcputils.NewMCPTool(mcpTool)
			Expect(tool).ToNot(BeNil())
		})
	})

	Describe("Declaration", func() {
		It("should return proper tool declaration", func() {
			mcpTool := server.ServerTool{
				Tool: mcp.Tool{
					Name:        "search_modules",
					Description: "Search for Terraform modules",
					InputSchema: mcp.ToolInputSchema{
						Type: "object",
						Properties: map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "Search query",
							},
						},
						Required: []string{"query"},
					},
				},
				Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{}, nil
				},
			}

			tool := mcputils.NewMCPTool(mcpTool)
			decl := tool.Declaration()

			Expect(decl.Name).To(Equal("search_modules"))
			Expect(decl.Description).To(Equal("Search for Terraform modules"))
			Expect(decl.InputSchema).ToNot(BeNil())
			Expect(decl.InputSchema.Type).To(Equal("object"))
			Expect(decl.InputSchema.Required).To(Equal([]string{"query"}))
			Expect(decl.InputSchema.Properties).To(HaveKey("query"))
		})
	})

	Describe("Call", func() {
		Context("with successful execution", func() {
			It("should execute the tool and return text content", func() {
				expectedResult := "module found: vpc-module"

				mcpTool := server.ServerTool{
					Tool: mcp.Tool{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: mcp.ToolInputSchema{
							Type: "object",
						},
					},
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						Expect(request.Params.Name).To(Equal("test_tool"))
						Expect(request.Params.Arguments).ToNot(BeNil())

						return &mcp.CallToolResult{
							Content: []mcp.Content{
								mcp.TextContent{
									Type: "text",
									Text: expectedResult,
								},
							},
						}, nil
					},
				}

				tool := mcputils.NewMCPTool(mcpTool)

				args := map[string]interface{}{
					"query": "vpc",
				}
				jsonArgs, err := json.Marshal(args)
				Expect(err).ToNot(HaveOccurred())

				result, err := tool.Call(context.Background(), jsonArgs)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(expectedResult))
			})

			It("should handle complex arguments", func() {
				var receivedArgs map[string]interface{}

				mcpTool := server.ServerTool{
					Tool: mcp.Tool{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: mcp.ToolInputSchema{
							Type: "object",
						},
					},
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						args, ok := request.Params.Arguments.(map[string]interface{})
						Expect(ok).To(BeTrue())
						receivedArgs = args
						return &mcp.CallToolResult{
							Content: []mcp.Content{
								mcp.TextContent{
									Type: "text",
									Text: "success",
								},
							},
						}, nil
					},
				}

				tool := mcputils.NewMCPTool(mcpTool)

				args := map[string]interface{}{
					"query":    "vpc",
					"provider": "aws",
					"limit":    10,
					"verified": true,
					"tags":     []string{"networking", "security"},
					"config": map[string]interface{}{
						"region": "us-east-1",
					},
				}
				jsonArgs, err := json.Marshal(args)
				Expect(err).ToNot(HaveOccurred())

				result, err := tool.Call(context.Background(), jsonArgs)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("success"))

				Expect(receivedArgs["query"]).To(Equal("vpc"))
				Expect(receivedArgs["provider"]).To(Equal("aws"))
				Expect(receivedArgs["limit"]).To(BeNumerically("==", 10))
				Expect(receivedArgs["verified"]).To(BeTrue())
				Expect(receivedArgs["tags"]).ToNot(BeNil())
				Expect(receivedArgs["config"]).ToNot(BeNil())
			})
		})

		Context("with errors", func() {
			It("should return error on invalid JSON", func() {
				mcpTool := server.ServerTool{
					Tool: mcp.Tool{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: mcp.ToolInputSchema{
							Type: "object",
						},
					},
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					},
				}

				tool := mcputils.NewMCPTool(mcpTool)

				invalidJSON := []byte("{invalid json")
				result, err := tool.Call(context.Background(), invalidJSON)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse input"))
				Expect(result).To(BeNil())
			})

			It("should propagate handler errors", func() {
				expectedError := errors.New("handler failed")

				mcpTool := server.ServerTool{
					Tool: mcp.Tool{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: mcp.ToolInputSchema{
							Type: "object",
						},
					},
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return nil, expectedError
					},
				}

				tool := mcputils.NewMCPTool(mcpTool)

				args := map[string]interface{}{}
				jsonArgs, err := json.Marshal(args)
				Expect(err).ToNot(HaveOccurred())

				result, err := tool.Call(context.Background(), jsonArgs)

				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(expectedError))
				Expect(result).To(BeNil())
			})
		})

		Context("with non-text content", func() {
			It("should return whole result for non-text content", func() {
				mcpTool := server.ServerTool{
					Tool: mcp.Tool{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: mcp.ToolInputSchema{
							Type: "object",
						},
					},
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{
							Content: []mcp.Content{
								mcp.ImageContent{
									Type: "image",
									Data: "base64data",
								},
							},
						}, nil
					},
				}

				tool := mcputils.NewMCPTool(mcpTool)

				args := map[string]interface{}{}
				jsonArgs, err := json.Marshal(args)
				Expect(err).ToNot(HaveOccurred())

				result, err := tool.Call(context.Background(), jsonArgs)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())

				callResult, ok := result.(*mcp.CallToolResult)
				Expect(ok).To(BeTrue())
				Expect(callResult.Content).To(HaveLen(1))
			})

			It("should return whole result for empty content", func() {
				mcpTool := server.ServerTool{
					Tool: mcp.Tool{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: mcp.ToolInputSchema{
							Type: "object",
						},
					},
					Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{
							Content: []mcp.Content{},
						}, nil
					},
				}

				tool := mcputils.NewMCPTool(mcpTool)

				args := map[string]interface{}{}
				jsonArgs, err := json.Marshal(args)
				Expect(err).ToNot(HaveOccurred())

				result, err := tool.Call(context.Background(), jsonArgs)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())

				callResult, ok := result.(*mcp.CallToolResult)
				Expect(ok).To(BeTrue())
				Expect(callResult.Content).To(BeEmpty())
			})
		})
	})
})
