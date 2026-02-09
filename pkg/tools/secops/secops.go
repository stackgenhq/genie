package secops

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/go-lib/encodeutils"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/snyk/policy-engine/pkg/models"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type secopsTool interface {
	tool.Tool
	CheckPolicy(ctx context.Context, req PolicyCheckRequest) (PolicyCheckResponse, error)
}

const (
	providerSNYK  = "snyk"
	providerTrivy = "trivy"
)

type SecOpsConfig struct {
	SeverityThresholds SeverityThresholds `yaml:"severity_thresholds" toml:"severity_thresholds"`
	Scanner            string             `yaml:"scanner" toml:"scanner"`
}

func DefaultSecOpsConfig() SecOpsConfig {
	return SecOpsConfig{
		SeverityThresholds: SeverityThresholds{
			High:   1,
			Medium: 10,
			Low:    -1,
		},
		Scanner: osutils.Getenv("GENIE_SECOPS_SCANNER", providerSNYK),
	}
}

func (s SecOpsConfig) MCPTool(ctx context.Context) (server.ServerTool, error) {
	toolProvider, err := s.Tool(ctx)
	if err != nil {
		return server.ServerTool{}, err
	}
	return server.ServerTool{
		Tool: mcp.NewTool("check_iac_policy",
			mcp.WithDescription("Scans infrastructure-as-code (IAC) files for security misconfigurations and policy violations."),
			mcp.WithString("iac_path",
				mcp.Required(),
				mcp.Description("Absolute path to the IaC file or directory to scan"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			iacSource, err := request.RequireString("iac_path")
			if err != nil {
				return nil, err
			}
			resp, err := toolProvider.CheckPolicy(ctx, PolicyCheckRequest{
				IACSource: iacSource,
			})
			if err != nil {
				return nil, err
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(string(encodeutils.MustToJSON(ctx, resp))),
				},
			}, nil
		},
	}, nil
}

func (s SecOpsConfig) MCPPrompts(ctx context.Context) []server.ServerPrompt {
	return []server.ServerPrompt{
		{
			Prompt: mcp.NewPrompt("review_infrastructure_security",
				mcp.WithPromptDescription("Review the infrastructure security for the given code"),
				mcp.WithArgument("iac_path",
					mcp.RequiredArgument(),
					mcp.ArgumentDescription("Absolute path to the IaC file or directory to scan"),
				),
			),
			Handler: func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				iacPath, ok := request.Params.Arguments["iac_path"]
				if !ok {
					return nil, fmt.Errorf("iac_path is required")
				}

				return &mcp.GetPromptResult{
					Description: "Review Infrastructure Security",
					Messages: []mcp.PromptMessage{
						{
							Role:    mcp.RoleUser,
							Content: mcp.NewTextContent(fmt.Sprintf("Please review the infrastructure security for the code at `%s`. \n1. First, analyze the infrastructure using `analyze_infrastructure` to understand the context. \n2. Then, scan for policy violations using `check_iac_policy`.\n3. Finally, summarize the findings and suggest remediations.", iacPath)),
						},
					},
				}, nil
			},
		},
	}
}

func (s SecOpsConfig) Tool(ctx context.Context) (secopsTool, error) {
	if strings.ToLower(s.Scanner) == providerTrivy {
		return newTrivyPolicyChecker(s)
	}
	return newSnykPolicyChecker(ctx, s)
}

type SeverityThresholds struct {
	High   int `yaml:"high" toml:"high"`
	Medium int `yaml:"medium" toml:"medium"`
	Low    int `yaml:"low" toml:"low"`
}

type PolicyCheckRequest struct {
	IACSource string `json:"iac_path"`
}

type PolicyCheckResponse struct {
	Compliant  bool
	Violations PolicyViolations
}

type PolicyViolations []PolicyViolation

// IsCompliant determines if the violations are within acceptable thresholds.
func (p PolicyViolations) isCompliant(thresholds SeverityThresholds) bool {
	// Count violations by severity
	highCount := 0
	mediumCount := 0
	lowCount := 0

	for _, v := range p {
		switch v.Severity {
		case "high", "critical":
			highCount++
		case "medium":
			mediumCount++
		case "low":
			lowCount++
		}
	}

	// Check thresholds (if threshold is -1, it means unlimited/ignored)
	if thresholds.High >= 0 && highCount > thresholds.High {
		return false
	}
	if thresholds.Medium >= 0 && mediumCount > thresholds.Medium {
		return false
	}
	if thresholds.Low >= 0 && lowCount > thresholds.Low {
		return false
	}

	return true
}

// NewViolations converts policy engine results to simplified PolicyViolation structs.
// Automatically deduplicates violations using a composite key of PolicyName|FilePath|Description.
func newViolations(results []models.Result) PolicyViolations {
	// Use a map to deduplicate violations
	// Key: PolicyName|FilePath|Description
	violationMap := make(map[string]PolicyViolation)

	for _, stateResult := range results {
		for _, ruleResult := range stateResult.RuleResults {
			for _, result := range ruleResult.Results {
				// Only include failed results
				if !result.Passed {
					// Extract file path from Resources or fall back to ResourceNamespace
					filePath := result.ResourceNamespace
					if len(result.Resources) != 0 && len(result.Resources[0].Location) != 0 {
						filePath = result.Resources[0].Location[0].Filepath
					}

					// Skip if no file path available
					if filePath == "" {
						continue
					}

					// Create unique key for deduplication
					key := fmt.Sprintf("%s|%s|%s",
						ruleResult.Id,
						filePath,
						result.Message)

					violationMap[key] = PolicyViolation{
						PolicyName:  ruleResult.Description,
						Description: result.Message,
						FilePath:    filePath,
						Severity:    result.Severity,
					}
				}
			}
		}
	}

	// Convert map to slice
	violations := make([]PolicyViolation, 0, len(violationMap))
	for _, v := range violationMap {
		violations = append(violations, v)
	}

	return violations
}

type PolicyViolation struct {
	PolicyName  string
	Description string
	FilePath    string
	Severity    string
}

func (p PolicyViolation) String() string {
	return fmt.Sprintf("%s: %s", p.PolicyName, p.Description)
}
