package cmd

import (
	"fmt"

	cceanalyzer "github.com/appcd-dev/cce/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/iacgen/generator"
	"github.com/appcd-dev/genie/pkg/tools/secops"
	"github.com/appcd-dev/genie/pkg/tools/tftools"
	"github.com/appcd-dev/go-lib/constants"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

type mcpCmd struct {
	rootOpts *rootCmdOption
}

func newMCPCommand(rootOpts *rootCmdOption) *cobra.Command {
	m := mcpCmd{rootOpts: rootOpts}
	return m.command()
}

func (m mcpCmd) command() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the Model Context Protocol (MCP) server",
		Long: `Start the Model Context Protocol (MCP) server to expose Genie's capabilities to MCP-compliant clients (e.g., Claude Desktop, IDEs).

This server operates over stdio and provides access to Terraform Registry tools.

Usage:
  Configure your MCP client to run this command:
  
  {
    "mcpServers": {
      "genie": {
        "command": "genie",
        "args": ["mcp"]
      }
    }
  }`,
		RunE: m.run,
	}
}

func (m mcpCmd) run(cmd *cobra.Command, args []string) error {
	// Create a new MCP server
	s := server.NewMCPServer("genie", constants.Version)

	// Initialize analyzer
	an, err := analyzer.New(cmd.Context(), "", cceanalyzer.New())
	if err == nil {
		analyzerTool := an.Tool()
		s.AddTool(analyzerTool.Tool, analyzerTool.Handler)
	}
	if tool, err := secops.DefaultSecOpsConfig().MCPTool(cmd.Context()); err != nil {
		logger.GetLogger(cmd.Context()).Warn("failed to initialize secops tool, skipping", "error", err)
	} else {
		s.AddTool(tool.Tool, tool.Handler)
	}
	tfValidationTool := tftools.TFValidator{}.Tool()
	s.AddTool(tfValidationTool.Tool, tfValidationTool.Handler)

	// Register Prompts
	secOpsPrompts := secops.DefaultSecOpsConfig().MCPPrompts(cmd.Context())
	for _, p := range secOpsPrompts {
		s.AddPrompt(p.Prompt, p.Handler)
	}

	iacGenPrompts := generator.MCPPrompts(cmd.Context())
	for _, p := range iacGenPrompts {
		s.AddPrompt(p.Prompt, p.Handler)
	}
	genieCfg, err := m.rootOpts.genieCfg()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	envBasedModelProvider := genieCfg.ModelConfig.NewEnvBasedModelProvider()
	if architect, err := generator.NewLLMBasedArchitect(cmd.Context(), envBasedModelProvider, genieCfg.Architect); err != nil {
		logger.GetLogger(cmd.Context()).Warn("failed to initialize architect tool, skipping", "error", err)
	} else {
		architectTool := architect.Tool()
		s.AddTool(architectTool.Tool, architectTool.Handler)
	}
	logger.GetLogger(cmd.Context()).Info("MCP server started", "toolCount", len(s.ListTools()))

	// Start the stdio server
	return server.ServeStdio(s)
}
