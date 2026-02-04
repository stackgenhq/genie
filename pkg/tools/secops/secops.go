package secops

import (
	"context"
	"fmt"
	"strings"

	"github.com/snyk/policy-engine/pkg/models"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type SecOpsConfig struct {
	SeverityThresholds SeverityThresholds `yaml:"severity_thresholds" toml:"severity_thresholds"`
	Scanner            string             `yaml:"scanner" toml:"scanner"`
}

func (s SecOpsConfig) Tool(ctx context.Context) (tool.Tool, error) {
	if strings.ToLower(s.Scanner) == "trivy" {
		return newTrivyPolicyChecker(ctx, s)
	}
	return newSnykPolicyChecker(ctx, s)
}

type SeverityThresholds struct {
	High   int `yaml:"high" toml:"high"`
	Medium int `yaml:"medium" toml:"medium"`
	Low    int `yaml:"low" toml:"low"`
}

type PolicyCheckRequest struct {
	IACSource string   `json:"iac_source"`
	Policies  []string `json:"policies,omitempty"`
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
func NewViolations(results []models.Result) PolicyViolations {
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
