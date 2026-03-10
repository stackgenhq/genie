// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcputils_test

import (
	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/mcputils"
)

var _ = Describe("ConvertMCPSchema", func() {
	Context("with basic types", func() {
		It("should convert simple object with string property", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "The name field",
					},
				},
				Required: []string{"name"},
			}

			result := mcputils.ConvertMCPSchema(input)

			Expect(result.Type).To(Equal("object"))
			Expect(result.Required).To(Equal([]string{"name"}))
			Expect(result.Properties).ToNot(BeNil())
			Expect(result.Properties).To(HaveKey("name"))
		})

		It("should convert object with multiple property types", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"count": map[string]any{
						"type":        "number",
						"description": "A count value",
					},
					"enabled": map[string]any{
						"type":    "boolean",
						"default": true,
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			Expect(result.Type).To(Equal("object"))
			Expect(result.Properties).To(HaveKey("count"))
			Expect(result.Properties).To(HaveKey("enabled"))
			Expect(result.Properties["count"].Type).To(Equal("number"))
			Expect(result.Properties["enabled"].Type).To(Equal("boolean"))
			Expect(result.Properties["enabled"].Default).To(Equal(true))
		})
	})

	Context("with nested properties", func() {
		It("should convert nested object structures", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"config": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"host": map[string]any{
								"type":        "string",
								"description": "The host address",
							},
							"port": map[string]any{
								"type":    "number",
								"default": 8080,
							},
						},
						"required": []any{"host"},
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			Expect(result.Properties).To(HaveKey("config"))
			configSchema := result.Properties["config"]
			Expect(configSchema.Type).To(Equal("object"))
			Expect(configSchema.Properties).To(HaveKey("host"))
			Expect(configSchema.Properties).To(HaveKey("port"))

			hostSchema := configSchema.Properties["host"]
			Expect(hostSchema.Type).To(Equal("string"))
			Expect(hostSchema.Description).To(Equal("The host address"))

			portSchema := configSchema.Properties["port"]
			Expect(portSchema.Type).To(Equal("number"))
			Expect(portSchema.Default).To(Equal(8080))
		})
	})

	Context("with array types", func() {
		It("should convert simple array of strings", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"tags": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			tagsSchema := result.Properties["tags"]
			Expect(tagsSchema.Type).To(Equal("array"))
			Expect(tagsSchema.Items).ToNot(BeNil())
			Expect(tagsSchema.Items.Type).To(Equal("string"))
		})

		It("should convert array of objects", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"configs": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"key": map[string]any{
									"type": "string",
								},
								"value": map[string]any{
									"type": "string",
								},
							},
						},
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			configsSchema := result.Properties["configs"]
			Expect(configsSchema.Type).To(Equal("array"))
			Expect(configsSchema.Items).ToNot(BeNil())
			Expect(configsSchema.Items.Type).To(Equal("object"))
			Expect(configsSchema.Items.Properties).To(HaveKey("key"))
			Expect(configsSchema.Items.Properties).To(HaveKey("value"))
		})
	})

	Context("with enum and default values", func() {
		It("should preserve enum and default values", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"environment": map[string]any{
						"type":        "string",
						"enum":        []any{"dev", "staging", "prod"},
						"default":     "dev",
						"description": "Deployment environment",
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			envSchema := result.Properties["environment"]
			Expect(envSchema.Type).To(Equal("string"))
			Expect(envSchema.Description).To(Equal("Deployment environment"))
			Expect(envSchema.Default).To(Equal("dev"))
			Expect(envSchema.Enum).To(HaveLen(3))
			Expect(envSchema.Enum).To(ContainElement("dev"))
			Expect(envSchema.Enum).To(ContainElement("staging"))
			Expect(envSchema.Enum).To(ContainElement("prod"))
		})
	})

	Context("with $defs", func() {
		It("should convert schema definitions", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"config": map[string]any{
						"$ref": "#/$defs/ConfigType",
					},
				},
				Defs: map[string]any{
					"ConfigType": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{
								"type": "string",
							},
						},
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			Expect(result.Type).To(Equal("object"))
			Expect(result.Defs).To(HaveKey("ConfigType"))

			configTypeDef := result.Defs["ConfigType"]
			Expect(configTypeDef.Type).To(Equal("object"))
			Expect(configTypeDef.Properties).To(HaveKey("name"))

			configProp := result.Properties["config"]
			Expect(configProp.Ref).To(Equal("#/$defs/ConfigType"))
		})
	})

	Context("with additionalProperties", func() {
		It("should preserve additionalProperties setting", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"metadata": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
			}

			result := mcputils.ConvertMCPSchema(input)

			metadataSchema := result.Properties["metadata"]
			Expect(metadataSchema.Type).To(Equal("object"))
			Expect(metadataSchema.AdditionalProperties).To(Equal(true))
		})
	})

	Context("with empty schema", func() {
		It("should handle empty schema gracefully", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
			}

			result := mcputils.ConvertMCPSchema(input)

			Expect(result.Type).To(Equal("object"))
			Expect(result.Properties).To(BeNil())
			Expect(result.Defs).To(BeNil())
			Expect(result.Required).To(BeNil())
		})
	})

	Context("with real-world complex schema", func() {
		It("should convert Terraform module search schema", func() {
			input := mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query for modules",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Filter by provider (e.g., 'aws', 'azure', 'gcp')",
						"enum":        []any{"aws", "azure", "gcp", "kubernetes"},
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of results",
						"default":     10,
					},
					"verified": map[string]any{
						"type":        "boolean",
						"description": "Only show verified modules",
						"default":     false,
					},
				},
				Required: []string{"query"},
			}

			result := mcputils.ConvertMCPSchema(input)

			Expect(result.Type).To(Equal("object"))
			Expect(result.Required).To(Equal([]string{"query"}))
			Expect(result.Properties).To(HaveLen(4))

			querySchema := result.Properties["query"]
			Expect(querySchema.Type).To(Equal("string"))
			Expect(querySchema.Description).To(Equal("Search query for modules"))

			providerSchema := result.Properties["provider"]
			Expect(providerSchema.Type).To(Equal("string"))
			Expect(providerSchema.Enum).To(HaveLen(4))

			limitSchema := result.Properties["limit"]
			Expect(limitSchema.Type).To(Equal("number"))
			Expect(limitSchema.Default).To(Equal(10))

			verifiedSchema := result.Properties["verified"]
			Expect(verifiedSchema.Type).To(Equal("boolean"))
			Expect(verifiedSchema.Default).To(Equal(false))
		})
	})
})
