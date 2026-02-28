package mcputils

import (
	"github.com/mark3labs/mcp-go/mcp"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ConvertMCPSchema converts MCP input schema to trpc-agent-go schema
// This function handles the conversion of MCP ToolInputSchema to the tool.Schema format
// required by trpc-agent-go, including all nested properties and JSON Schema features.
func ConvertMCPSchema(mcpSchema mcp.ToolInputSchema) *tool.Schema {
	schema := &tool.Schema{
		Type:     mcpSchema.Type,
		Required: mcpSchema.Required,
	}

	// Convert properties from map[string]any to map[string]*Schema
	if len(mcpSchema.Properties) > 0 {
		schema.Properties = make(map[string]*tool.Schema)
		for key, value := range mcpSchema.Properties {
			schema.Properties[key] = convertPropertySchema(value)
		}
	}

	// Convert $defs if present
	if len(mcpSchema.Defs) > 0 {
		schema.Defs = make(map[string]*tool.Schema)
		for key, value := range mcpSchema.Defs {
			schema.Defs[key] = convertPropertySchema(value)
		}
	}

	return schema
}

// convertPropertySchema converts a property value (map[string]any) to *tool.Schema
// This is a recursive function that handles nested objects and arrays.
func convertPropertySchema(prop any) *tool.Schema {
	propMap, ok := prop.(map[string]any)
	if !ok {
		// If it's not a map, return a basic schema
		return &tool.Schema{Type: "string"}
	}

	schema := &tool.Schema{}

	// Extract common fields
	if t, ok := propMap["type"].(string); ok {
		schema.Type = t
	}
	if desc, ok := propMap["description"].(string); ok {
		schema.Description = desc
	}
	if def, ok := propMap["default"]; ok {
		schema.Default = def
	}
	if enum, ok := propMap["enum"].([]any); ok {
		schema.Enum = enum
	}
	if ref, ok := propMap["$ref"].(string); ok {
		schema.Ref = ref
	}

	// Handle required array
	if required, ok := propMap["required"].([]any); ok {
		schema.Required = make([]string, 0, len(required))
		for _, r := range required {
			if rStr, ok := r.(string); ok {
				schema.Required = append(schema.Required, rStr)
			}
		}
	}

	// Handle nested properties (for object types)
	if props, ok := propMap["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*tool.Schema)
		for key, value := range props {
			schema.Properties[key] = convertPropertySchema(value)
		}
	}

	// Handle items (for array types)
	if items, ok := propMap["items"]; ok {
		schema.Items = convertPropertySchema(items)
	}

	// Handle additionalProperties
	if addProps, ok := propMap["additionalProperties"]; ok {
		schema.AdditionalProperties = addProps
	}

	return schema
}
