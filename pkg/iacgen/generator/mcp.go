package generator

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPPrompts returns the MCP prompts for the generator package
func MCPPrompts(ctx context.Context) []server.ServerPrompt {
	return []server.ServerPrompt{
		{
			Prompt: mcp.NewPrompt("generate_and_validate",
				mcp.WithPromptDescription("Generate and validate infrastructure code"),
				mcp.WithArgument("architecture_requirements",
					mcp.RequiredArgument(),
					mcp.ArgumentDescription("Description of the infrastructure to generate"),
				),
				mcp.WithArgument("output_path",
					mcp.RequiredArgument(),
					mcp.ArgumentDescription("Absolute path where the code should be generated"),
				),
			),
			Handler: func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				requirements, ok := request.Params.Arguments["architecture_requirements"]
				if !ok {
					return nil, fmt.Errorf("architecture_requirements is required")
				}
				outputPath, ok := request.Params.Arguments["output_path"]
				if !ok {
					return nil, fmt.Errorf("output_path is required")
				}

				return &mcp.GetPromptResult{
					Description: "Generate and Validate Infrastructure",
					Messages: []mcp.PromptMessage{
						{
							Role: mcp.RoleUser,
							Content: mcp.NewTextContent(fmt.Sprintf(`Please generate infrastructure code based on these requirements: `+"`%s`"+`. 
Save the code to `+"`%s`"+`. 

1. Use `+"`generate_iac`"+` (if available) or existing tools to create the files. 
2. After generation, validate the code using `+"`validate_iac`"+`. 
3. Fix any validation errors.`, requirements, outputPath)),
						},
					},
				}, nil
			},
		},
	}
}
