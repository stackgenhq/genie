// Package mcputils provides utilities for integrating MCP (Model Context Protocol) tools
// with trpc-agent-go. It handles the conversion of MCP tool schemas and provides
// wrapper types to make MCP tools compatible with the trpc-agent-go tool interface.
//
// The main components are:
//   - ConvertMCPSchema: Converts MCP ToolInputSchema to trpc-agent-go tool.Schema
//   - MCPTool: Wraps MCP ServerTool to implement tool.Tool interface
//
// Example usage:
//
//	import (
//	    "github.com/appcd-dev/genie/pkg/mcputils"
//	    "github.com/hashicorp/terraform-mcp-server/pkg/tools/registry"
//	    "github.com/sirupsen/logrus"
//	)
//
//	logger := logrus.New()
//	mcpServerTool := registry.SearchModules(logger)
//	trpcTool := mcputils.NewMCPTool(mcpServerTool, logger)
package mcputils
