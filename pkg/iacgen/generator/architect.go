package generator

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/google/search"
)

//go:embed prompts/architect_persona.txt
var architectPersonaPrompt string

//counterfeiter:generate . Architect
type Architect interface {
	Design(ctx context.Context, req DesignCloudRequest) (DesignCloudResponse, error)
	Tool() server.ServerTool
}

type DesignCloudRequest struct {
	MethodCalls analyzer.MappedResources
	SaveTo      string
	EventChan   chan<- interface{}
}

type DesignCloudResponse struct {
	Notes []string
}

type ArchitectConfig struct {
	GoogleSearchAPIKey string `yaml:"google_search_api_key" toml:"google_search_api_key"`
	GoogleSearchCX     string `yaml:"google_search_cx" toml:"google_search_cx"`
	PageSize           int    `yaml:"page_size" toml:"page_size"`
}

func (a ArchitectConfig) searchConfig() []search.Option {
	var opts []search.Option
	if a.GoogleSearchAPIKey != "" {
		opts = append(opts, search.WithAPIKey(a.GoogleSearchAPIKey))
	}
	if a.GoogleSearchCX != "" {
		opts = append(opts, search.WithEngineID(a.GoogleSearchCX))
	}
	if a.PageSize > 0 {
		opts = append(opts, search.WithSize(a.PageSize))
	}
	return opts
}

func NewLLMBasedArchitect(ctx context.Context, modelProvider modelprovider.ModelProvider, cfg ArchitectConfig) (Architect, error) {
	var toolsList []tool.Tool
	if cfg.GoogleSearchAPIKey != "" {
		if searchTool, err := search.NewToolSet(ctx, cfg.searchConfig()...); err == nil {
			toolsList = append(toolsList, searchTool.Tools(ctx)...)
		}
	}

	expertBio := expert.ExpertBio{
		Personality: langfuse.GetPrompt(ctx, "genie_architect_persona", architectPersonaPrompt),
		Name:        "software-architect",
		Description: "Software Architect",
		Tools:       toolsList,
	}
	expert, err := expertBio.ToExpert(ctx, modelProvider)
	if err != nil {
		return nil, err
	}

	return llmBasedArchitect{
		expert: expert,
	}, nil
}

type llmBasedArchitect struct {
	expert expert.Expert
}

func (a llmBasedArchitect) Tool() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("generate_architecture",
			mcp.WithDescription("Generates architectural insights and design patterns based on code analysis (CCE)."),
			mcp.WithString("cce_file_path",
				mcp.Required(),
				mcp.Description("Absolute path to the CCE analysis result file (NDJSON)"),
			),
			mcp.WithString("save_to",
				mcp.Required(),
				mcp.Description("Absolute path to the directory where architecture notes will be saved"),
			),
		),
		Handler: a.toolCall,
	}
}

func (a llmBasedArchitect) toolCall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cceFilePath, err := req.RequireString("cce_file_path")
	if err != nil {
		return nil, err
	}
	saveTo, err := req.RequireString("save_to")
	if err != nil {
		return nil, err
	}

	designReq := DesignCloudRequest{
		SaveTo: saveTo,
	}
	designReq.MethodCalls, err = a.mappedResources(ctx, cceFilePath)
	if err != nil {
		return nil, err
	}

	resp, err := a.Design(ctx, designReq)
	if err != nil {
		return nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(strings.Join(resp.Notes, "\n\n")),
		},
	}, nil
}

func (llmBasedArchitect) mappedResources(ctx context.Context, cceNDJSONFilePath string) (analyzer.MappedResources, error) {
	file, err := os.Open(cceNDJSONFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CCE file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.GetLogger(ctx).Warn("failed to close CCE file", "error", closeErr)
		}
	}()

	var methodCalls analyzer.MappedResources
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var resource analyzer.MappedResource
		if err := json.Unmarshal(scanner.Bytes(), &resource); err != nil {
			logger.GetLogger(ctx).Warn("failed to unmarshal CCE resource, skipping line", "error", err)
			continue // Skip invalid lines
		}
		methodCalls = append(methodCalls, resource)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading CCE file: %w", err)
	}
	return methodCalls, nil
}

func (a llmBasedArchitect) Design(ctx context.Context, req DesignCloudRequest) (DesignCloudResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedArchitect.Design")
	logr.Info("creating architecture design")
	defer func(startTime time.Time) {
		logr.Info("architecture design phase complete", "duration", time.Since(startTime).String())
	}(time.Now())
	cloudBasedPrompts := req.MethodCalls.Summarize(ctx)
	if len(cloudBasedPrompts) == 0 {
		return DesignCloudResponse{
			Notes: []string{
				"There are no resources to generate IAC for.",
			},
		}, nil
	}
	multiCloudNotes := sync.Map{}
	errGroup, gctx := errgroup.WithContext(ctx)
	for _, cloudBasedPrompt := range cloudBasedPrompts {
		errGroup.Go(func() error {
			logr.Info("Generating architecture design for cloud provider", "cloudProvider", cloudBasedPrompt.CloudProvider, "identifiedResources", len(cloudBasedPrompt.Prompt))
			notes, err := a.generateArchitecture(gctx, cloudBasedPrompt, req.EventChan)
			if err != nil {
				return err
			}
			logr.Info("architecture design phase complete", "cloudProvider", cloudBasedPrompt.CloudProvider)
			multiCloudNotes.Store(cloudBasedPrompt.CloudProvider, notes)
			return nil
		})
	}
	err := errGroup.Wait()
	if err != nil {
		return DesignCloudResponse{}, err
	}
	result := DesignCloudResponse{}
	multiCloudNotes.Range(func(key, value interface{}) bool {
		result.Notes = append(result.Notes, value.(string))
		return true
	})
	if err = a.saveNotes(ctx, req.SaveTo, result.Notes); err != nil {
		logr.Warn("error saving the architect notes", "error", err)
	}

	// Generate README.md after notes are completed
	logr.Info("Generating README.md", "saveTo", req.SaveTo)
	if err := a.generateReadme(ctx, req.SaveTo, result); err != nil {
		logr.Error("Failed to generate README.md", "error", err)
	}
	return result, nil
}

func (a llmBasedArchitect) saveNotes(ctx context.Context, saveTo string, notes []string) error {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedArchitect.saveNotes")
	logr.Info("Saving notes", "saveTo", saveTo, "notes", notes)
	return os.WriteFile(filepath.Join(saveTo, "Architecture.md"), []byte(strings.Join(notes, "\n")), 0644)
}

func (a llmBasedArchitect) generateArchitecture(ctx context.Context, cloudInfraDetails analyzer.CloudBasedPrompt, eventChan chan<- interface{}) (string, error) {
	// Get the resource categorizer instance
	result, err := a.expert.Do(ctx, expert.Request{
		Message:      cloudInfraDetails.Prompt,
		EventChannel: eventChan,
		TaskType:     modelprovider.TaskNovelReasoning,
		Mode:         expert.CostOptimizedConfig(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get a plan from architect: %w", err)
	}
	notes := strings.Builder{}
	for i := range result.Choices {
		notes.WriteString(result.Choices[i].Message.Content)
	}
	return notes.String(), nil
}

// generateReadme creates a comprehensive README.md file in the SaveTo directory using LLM
// The README is designed to be technically sound, context-aware, and understandable by junior developers
func (a llmBasedArchitect) generateReadme(ctx context.Context, saveTo string, response DesignCloudResponse) error {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedArchitect.generateReadme")

	// Ensure the directory exists
	if err := os.MkdirAll(saveTo, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", saveTo, err)
	}

	// Build a prompt for the LLM to generate a context-aware README
	prompt := buildReadmePrompt(response.Notes)

	// Use the expert to generate the README content
	logr.Info("Generating README.md using LLM", "saveTo", saveTo)
	var readmeContent strings.Builder
	_, err := a.expert.Do(ctx, expert.Request{
		Message:  prompt,
		TaskType: modelprovider.TaskPlanning,
		ChoiceProcessor: func(choices ...model.Choice) {
			for _, choice := range choices {
				readmeContent.WriteString(choice.Message.Content)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("failed to generate README content: %w", err)
	}

	// Write README to file
	readmePath := filepath.Join(saveTo, "README.md")
	if err := os.WriteFile(readmePath, []byte(readmeContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write README.md: %w", err)
	}

	logr.Info("README.md created successfully", "path", readmePath)
	return nil
}

// buildReadmePrompt creates a prompt for generating a context-aware README
func buildReadmePrompt(notes []string) string {
	var prompt strings.Builder

	prompt.WriteString("**Task:** Generate a comprehensive README.md for Infrastructure as Code\n\n")

	prompt.WriteString("**Role:** You are a senior DevOps engineer creating documentation for a junior developer.\n\n")

	prompt.WriteString("**Context:**\n")
	prompt.WriteString("Infrastructure as Code has been generated based on architectural analysis. ")
	prompt.WriteString("Create a README.md that explains this infrastructure in a way that is:\n")
	prompt.WriteString("- Technically accurate and sound\n")
	prompt.WriteString("- Easy to understand for junior developers\n")
	prompt.WriteString("- Specific to the actual architecture (not generic)\n")
	prompt.WriteString("- Actionable with clear deployment instructions\n\n")

	// Include architecture recommendations
	prompt.WriteString("**Architecture Recommendations:**\n\n")
	for i, note := range notes {
		prompt.WriteString(fmt.Sprintf("### Cloud Provider %d:\n", i+1))
		prompt.WriteString(note)
		prompt.WriteString("\n\n")
	}

	// Instructions for README structure
	prompt.WriteString("**README Structure Requirements:**\n\n")
	prompt.WriteString("Generate a complete README.md in markdown format with these sections:\n\n")

	prompt.WriteString("1. **Header and Overview**\n")
	prompt.WriteString("   - Title: \"Infrastructure as Code - Architecture Documentation\"\n")
	prompt.WriteString("   - Include generation timestamp\n")
	prompt.WriteString("   - Brief overview of what this infrastructure does (based on the architecture notes)\n\n")

	prompt.WriteString("2. **Architecture Summary**\n")
	prompt.WriteString("   - Summarize the key architectural decisions from the notes above\n")
	prompt.WriteString("   - Explain the architecture pattern being used (e.g., microservices, serverless, etc.)\n")
	prompt.WriteString("   - Describe how components interact (based on the actual resources identified)\n\n")

	prompt.WriteString("3. **Cloud Providers**\n")
	prompt.WriteString("   - List the cloud providers being used\n")
	prompt.WriteString("   - Explain what resources are deployed on each provider\n\n")

	prompt.WriteString("4. **Key Components**\n")
	prompt.WriteString("   - List and explain the main infrastructure components (based on actual resource types)\n")
	prompt.WriteString("   - For each component, explain its purpose in this specific architecture\n\n")

	prompt.WriteString("5. **Prerequisites**\n")
	prompt.WriteString("   - Terraform/OpenTofu installation (version 1.0+)\n")
	prompt.WriteString("   - Cloud provider credentials (specific to the providers being used)\n")
	prompt.WriteString("   - Required permissions (be specific based on the resources)\n\n")

	prompt.WriteString("6. **Deployment Instructions**\n")
	prompt.WriteString("   - Step-by-step guide to deploy this specific infrastructure\n")
	prompt.WriteString("   - Include terraform init, plan, apply commands\n")
	prompt.WriteString("   - Mention any provider-specific setup needed\n\n")

	prompt.WriteString("7. **File Structure**\n")
	prompt.WriteString("   - Explain the standard Terraform file organization\n")
	prompt.WriteString("   - main.tf, variables.tf, outputs.tf, versions.tf, etc.\n\n")

	prompt.WriteString("8. **Understanding the Code (For Junior Developers)**\n")
	prompt.WriteString("   - Explain key Terraform/IaC concepts with examples from THIS architecture\n")
	prompt.WriteString("   - Resources, Variables, Modules, Outputs\n")
	prompt.WriteString("   - Use actual resource types from the architecture when giving examples\n\n")

	prompt.WriteString("9. **Configuration**\n")
	prompt.WriteString("   - Explain what variables can be customized\n")
	prompt.WriteString("   - Provide guidance on common configuration scenarios\n\n")

	prompt.WriteString("10. **Security Considerations**\n")
	prompt.WriteString("    - Based on the architecture notes, highlight security best practices\n")
	prompt.WriteString("    - IAM permissions, encryption, network isolation, etc.\n\n")

	prompt.WriteString("11. **Operational Best Practices**\n")
	prompt.WriteString("    - Version control, state management, workspace usage\n")
	prompt.WriteString("    - Monitoring and logging recommendations\n\n")

	prompt.WriteString("12. **Troubleshooting**\n")
	prompt.WriteString("    - Common issues specific to this architecture\n")
	prompt.WriteString("    - Provider-specific troubleshooting tips\n\n")

	prompt.WriteString("13. **Cleanup**\n")
	prompt.WriteString("    - How to destroy resources\n")
	prompt.WriteString("    - Warnings about data loss\n\n")

	prompt.WriteString("14. **Additional Resources**\n")
	prompt.WriteString("    - Links to Terraform documentation\n")
	prompt.WriteString("    - Cloud provider documentation\n")
	prompt.WriteString("    - Relevant module documentation\n\n")

	prompt.WriteString("**Important Guidelines:**\n")
	prompt.WriteString("- Write in clear, professional markdown\n")
	prompt.WriteString("- Use code blocks with proper syntax highlighting (```hcl, ```bash)\n")
	prompt.WriteString("- Reference ACTUAL components from the architecture, not generic examples\n")
	prompt.WriteString("- Explain WHY things are done, not just HOW\n")
	prompt.WriteString("- Make it educational for junior developers while remaining technically accurate\n")
	prompt.WriteString("- Include the current timestamp: " + time.Now().Format("2006-01-02 15:04:05 MST") + "\n\n")

	prompt.WriteString("Generate the complete README.md content now.\n")

	return prompt.String()
}
