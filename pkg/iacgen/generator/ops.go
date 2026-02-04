package generator

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/genie/pkg/tools/secops"
	"github.com/appcd-dev/genie/pkg/tools/tftools"
	"github.com/appcd-dev/go-lib/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
)

//go:generate go tool counterfeiter -generate

//go:embed prompts/ops_persona.txt
var opsPersonaPrompt string

// IACRequest represents a request to generate Infrastructure as Code
type IACRequest struct {
	// ArchitectureRequirement contains the architecture notes from the architect
	// This should include component descriptions, workflow patterns, and architectural decisions
	ArchitectureRequirement []string

	// OutputFolder
	OutputFolder string

	// EventChan is an optional channel for emitting events during IaC generation
	EventChan chan<- interface{}
}

// IACResponse represents the result of IaC generation
type IACResponse struct {
	// IACCodePath contains the location to the generated IAC
	IACCodePath string

	// Notes contains additional information about the generation process
	Notes []string
}

// IACWriter generates Infrastructure as Code from architectural requirements
//
//counterfeiter:generate . IACWriter
type IACWriter interface {
	CreateIAC(ctx context.Context, requirement IACRequest) (IACResponse, error)
}

type OpsConfig struct {
	MaxPages            int  `yaml:"max_pages" toml:"max_pages"`
	EnableVerification  bool `yaml:"enable_verification" toml:"enable_verification"`
	MaxVerificationRuns int  `yaml:"max_verification_runs" toml:"max_verification_runs"`
}

// NewLLMBasedIACWriter creates a new LLM-based IaC writer with Terraform MCP tool integration
func NewLLMBasedIACWriter(ctx context.Context, modelProvider modelprovider.ModelProvider, cfg OpsConfig, secOpsCfg secops.SecOpsConfig) (IACWriter, error) {
	logr := logger.GetLogger(ctx).With("fn", "NewLLMBasedIACWriter")
	// Create Terraform registry tools
	terraformTools := NewTerraformTools(cfg.MaxPages)
	terraformToolsList := terraformTools.GetTools()

	// Combine all tools
	allTools := make([]tool.Tool, 0, len(terraformToolsList)+2)
	allTools = append(allTools, terraformToolsList...)
	allTools = append(allTools, &tftools.TFValidator{})
	// Create validation and policy checking tools
	policyChecker, err := secOpsCfg.Tool(ctx)
	if err != nil {
		logr.Warn("Failed to create policy checker", "error", err)
	} else {
		allTools = append(allTools, policyChecker)
	}

	expertBio := expert.ExpertBio{
		Personality: langfuse.GetPrompt(ctx, "genie_ops_persona", opsPersonaPrompt),
		Name:        "terraform-expert",
		Description: "Terraform/OpenTofu Infrastructure as Code Expert with Validation and Policy Checking",
		Tools:       allTools,
	}

	expertInstance, err := expertBio.ToExpert(ctx, modelProvider)
	if err != nil {
		logr.Error("Failed to create expert instance", "error", err)
		return nil, err
	}

	return &llmBasedIACWriter{
		expert: expertInstance,
		cfg:    cfg,
	}, nil
}

type llmBasedIACWriter struct {
	expert       expert.Expert
	cfg          OpsConfig
	outputFolder string
}

// CreateIAC generates Terraform code based on architectural requirements
// The LLM expert will use the provided tools to search for modules and write files
func (w *llmBasedIACWriter) CreateIAC(ctx context.Context, requirement IACRequest) (IACResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedIACWriter.CreateIAC")
	logr.Info("Generating Terraform code from architecture requirements")

	defer func(startTime time.Time) {
		logr.Info("Terraform code generation completed", "duration", time.Since(startTime).String())
	}(time.Now())

	// Validate input
	logr.Debug("Validating input requirements")
	if len(requirement.ArchitectureRequirement) == 0 {
		return IACResponse{}, fmt.Errorf("architecture requirement cannot be empty")
	}

	if len(requirement.OutputFolder) == 0 {
		logr.Error("Output folder is empty")
		return IACResponse{}, fmt.Errorf("output folder cannot be empty")
	}

	// Create file toolset using trpc-agent-go/tool/file
	fileToolSet, err := file.NewToolSet(
		file.WithBaseDir(requirement.OutputFolder),
	)
	if err != nil {
		logr.Error("Failed to create file toolset", "error", err)
		return IACResponse{}, fmt.Errorf("failed to create file toolset: %w", err)
	}
	fileToolsList := fileToolSet.Tools(ctx)
	// Build the prompt for Terraform code generation with module-first approach
	logr.Debug("Building module-first prompt")
	prompt := buildModuleFirstPrompt(requirement, w.cfg)
	logr.Info("Prompt built", "promptLength", len(prompt))

	// Generate Terraform code using the expert with available tools
	// The expert will automatically discover and use the appropriate tools for registry search and file operations
	logr.Info("Invoking expert to generate Terraform code")
	result, err := w.expert.Do(ctx, expert.Request{
		Message:         prompt,
		AdditionalTools: fileToolsList,
		EventChannel:    requirement.EventChan,
	})
	if err != nil {
		logr.Error("Expert failed to generate Terraform code", "error", err)
		return IACResponse{}, fmt.Errorf("failed to generate Terraform code: %w", err)
	}

	logr.Info("Expert completed code generation", "choicesCount", len(result.Choices))

	// Collect the generated code and notes from all choices
	var notes []string
	notes = append(notes, "Terraform code generated using module-first approach")
	notes = append(notes, fmt.Sprintf("Files written to: %s", requirement.OutputFolder))

	response := IACResponse{
		IACCodePath: requirement.OutputFolder,
		Notes:       notes,
	}

	logr.Info("Terraform code generation successful", "outputFolder", requirement.OutputFolder)

	return response, nil
}

// preApprovedAWSModules contains well-known AWS Terraform modules with their versions.
// Using these directly avoids expensive search and detail-fetch operations.
var preApprovedAWSModules = map[string]string{
	"vpc":          "terraform-aws-modules/vpc/aws v5.17.0",
	"s3-bucket":    "terraform-aws-modules/s3-bucket/aws v4.6.0",
	"sqs":          "terraform-aws-modules/sqs/aws v4.3.0",
	"autoscaling":  "terraform-aws-modules/autoscaling/aws v9.1.0",
	"kms":          "terraform-aws-modules/kms/aws v4.2.0",
	"eventbridge":  "terraform-aws-modules/eventbridge/aws v4.3.0",
	"ec2-instance": "terraform-aws-modules/ec2-instance/aws v5.7.1",
	"iam":          "terraform-aws-modules/iam/aws v5.52.2",
	"rds":          "terraform-aws-modules/rds/aws v6.10.0",
	"lambda":       "terraform-aws-modules/lambda/aws v7.20.1",
}

// preApprovedAzureModules contains well-known Azure Terraform modules with their versions.
// These are from Azure Verified Modules (AVM) and terraform-azure-modules organization.
var preApprovedAzureModules = map[string]string{
	"vnet":             "Azure/vnet/azurerm v4.1.0",
	"aks":              "Azure/aks/azurerm v9.4.1",
	"resource-group":   "Azure/avm-res-resources-resourcegroup/azurerm v0.2.1",
	"storage-account":  "Azure/avm-res-storage-storageaccount/azurerm v0.4.1",
	"key-vault":        "Azure/avm-res-keyvault-vault/azurerm v0.10.0",
	"virtual-machine":  "Azure/avm-res-compute-virtualmachine/azurerm v0.18.0",
	"postgresql":       "Azure/avm-res-dbforpostgresql-flexibleserver/azurerm v0.4.0",
	"container-app":    "Azure/avm-res-app-containerapp/azurerm v0.5.0",
	"service-bus":      "Azure/avm-res-servicebus-namespace/azurerm v0.4.0",
	"cosmos-db":        "Azure/avm-res-documentdb-databaseaccount/azurerm v0.10.0",
	"app-service":      "Azure/avm-res-web-site/azurerm v0.17.0",
	"function-app":     "Azure/avm-res-web-site/azurerm v0.17.0",
	"application-gw":   "Azure/avm-res-network-applicationgateway/azurerm v0.4.0",
	"private-endpoint": "Azure/avm-res-network-privateendpoint/azurerm v0.10.0",
	"network-security": "Azure/avm-res-network-networksecuritygroup/azurerm v0.5.0",
}

// preApprovedGCPModules contains well-known GCP Terraform modules with their versions.
// These are from the terraform-google-modules organization.
var preApprovedGCPModules = map[string]string{
	"project-factory":   "terraform-google-modules/project-factory/google v17.0.0",
	"network":           "terraform-google-modules/network/google v9.3.0",
	"cloud-storage":     "terraform-google-modules/cloud-storage/google v8.1.0",
	"gke":               "terraform-google-modules/kubernetes-engine/google v35.0.1",
	"cloud-sql":         "terraform-google-modules/sql-db/google v22.2.0",
	"iam":               "terraform-google-modules/iam/google v8.0.0",
	"pubsub":            "terraform-google-modules/pubsub/google v7.0.0",
	"cloud-function":    "terraform-google-modules/event-function/google v4.1.0",
	"cloud-run":         "terraform-google-modules/cloud-run/google v0.14.0",
	"service-accounts":  "terraform-google-modules/service-accounts/google v4.4.3",
	"vpc":               "terraform-google-modules/network/google v9.3.0",
	"load-balancer":     "terraform-google-modules/lb-http/google v12.0.0",
	"memorystore-redis": "terraform-google-modules/memorystore/google v12.0.0",
	"secret-manager":    "terraform-google-modules/secret-manager/google v0.7.0",
	"cloud-armor":       "terraform-google-modules/cloud-armor/google v3.0.1",
}

// getPreApprovedModulesSection returns a formatted string of pre-approved modules
func getPreApprovedModulesSection() string {
	var sb strings.Builder

	sb.WriteString("\n**AWS Modules:**\n")
	for name, source := range preApprovedAWSModules {
		sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", name, source))
	}

	sb.WriteString("\n**Azure Modules:**\n")
	for name, source := range preApprovedAzureModules {
		sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", name, source))
	}

	sb.WriteString("\n**GCP Modules:**\n")
	for name, source := range preApprovedGCPModules {
		sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", name, source))
	}

	return sb.String()
}

// buildModuleFirstPrompt creates a detailed prompt emphasizing module-first approach
// with cost optimizations to reduce token usage and tool calls.
func buildModuleFirstPrompt(requirement IACRequest, cfg OpsConfig) string {
	var prompt strings.Builder

	prompt.WriteString("**OBJECTIVE:** Generate complete, working Terraform code files.\n\n")

	prompt.WriteString("**SUCCESS CRITERIA:**\n")
	prompt.WriteString("✅ All `.tf` files written via `save_file`\n")

	if cfg.EnableVerification {
		prompt.WriteString("✅ `iac-validator` passes\n")
		prompt.WriteString("✅ `terraform-validate` passes\n")
		prompt.WriteString("✅ `check_iac_policy` passes\n\n")
	} else {
		prompt.WriteString("\n")
	}

	if requirement.OutputFolder != "" {
		prompt.WriteString(fmt.Sprintf("**Output Folder:** %s\n", requirement.OutputFolder))
		prompt.WriteString("Use ONLY relative filenames with `save_file` (e.g., 'main.tf').\n\n")
	}

	// Pre-approved modules section - biggest cost saver
	prompt.WriteString("**PRE-APPROVED MODULES (USE DIRECTLY - DO NOT SEARCH OR FETCH DETAILS):**\n")
	prompt.WriteString(getPreApprovedModulesSection())
	prompt.WriteString("\nFor these modules, use the source/version directly. Do NOT call `search_modules` or `get_module_details`.\n\n")

	prompt.WriteString("**Architecture Requirements:**\n")
	prompt.WriteString(strings.Join(requirement.ArchitectureRequirement, "\n"))
	prompt.WriteString("\n\n")

	// Simplified workflow
	prompt.WriteString("**WORKFLOW:**\n")
	prompt.WriteString("1. Use pre-approved modules directly (skip search/details)\n")
	prompt.WriteString("2. Only search for modules NOT in pre-approved list\n")
	prompt.WriteString("3. Write ALL files in ONE batch using `save_file`\n")

	if cfg.EnableVerification {
		prompt.WriteString("4. Validate with all three tools\n")
		prompt.WriteString(fmt.Sprintf("5. Fix and re-validate if needed (Max %d runs)\n\n", cfg.MaxVerificationRuns))
	} else {
		prompt.WriteString("\n")
	}

	// Strict cost constraints
	prompt.WriteString("**STRICT CONSTRAINTS (MANDATORY):**\n")
	prompt.WriteString("- Complete in **5 tool calls or fewer**\n")
	prompt.WriteString("- **NEVER** call `get_module_details` for pre-approved modules\n")
	prompt.WriteString("- Write ALL files in a SINGLE batch operation\n")
	prompt.WriteString("- Do NOT make redundant searches\n\n")

	// Validation paths
	if cfg.EnableVerification {
		prompt.WriteString("**VALIDATION:**\n")
		prompt.WriteString(fmt.Sprintf("- `validate_iac`: iac_file_path='%s'\n", requirement.OutputFolder))
		prompt.WriteString(fmt.Sprintf("- `check_iac_policy`: iac_source='%s'\n\n", requirement.OutputFolder))
	}

	prompt.WriteString("**BEGIN NOW:** Generate and write ALL Terraform files immediately using pre-approved modules.\n")

	return prompt.String()
}
