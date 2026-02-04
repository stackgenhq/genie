package secops

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/appcd-dev/go-lib/logger"
	"github.com/aquasecurity/trivy/pkg/iac/scan"
	"github.com/aquasecurity/trivy/pkg/iac/scanners/terraform"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TrivyScanner defines the interface for scanning filesystems.
type TrivyScanner interface {
	ScanFS(ctx context.Context, target fs.FS, path string) (scan.Results, error)
}

// NewTrivyPolicyChecker creates a new policy checker using Trivy.
func newTrivyPolicyChecker(ctx context.Context, ops SecOpsConfig) (TrivyPolicyChecker, error) {
	return TrivyPolicyChecker{
		cfg:     ops,
		scanner: terraform.New(),
	}, nil
}

type TrivyPolicyChecker struct {
	cfg     SecOpsConfig
	scanner TrivyScanner
}

func (p TrivyPolicyChecker) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "check_iac_policy",
		Description: "Check Terraform/OpenTofu code against security policies to identify compliance violations using Trivy",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"iac_source": {
					Type:        "string",
					Description: "Absolute path to the directory containing Terraform/OpenTofu .tf files to check",
				},
			},
			Required: []string{"iac_source"},
		},
	}
}

func (p TrivyPolicyChecker) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var req PolicyCheckRequest
	if err := json.Unmarshal(jsonArgs, &req); err != nil {
		return nil, err
	}
	return p.CheckPolicy(ctx, req)
}

func (p TrivyPolicyChecker) CheckPolicy(ctx context.Context, req PolicyCheckRequest) (PolicyCheckResponse, error) {
	logr := logger.GetLogger(ctx)

	// Trivy scanner works with fs.FS. We use afero to wrap the OS filesystem,
	// but terraform.Scanner.ScanFS expects a standard fs.FS.
	// We map the absolute path to a fs.FS rooted at that path.
	// Since req.IACSource is an absolute path, we can use os.DirFS.
	// Ensure the path exists.
	if _, err := os.Stat(req.IACSource); os.IsNotExist(err) {
		return PolicyCheckResponse{}, fmt.Errorf("iac_source path does not exist: %s", req.IACSource)
	}

	targetFS := os.DirFS(req.IACSource)

	logr.Info("starting trivy scan", "path", req.IACSource)

	// Scan the filesystem
	// The second argument is the path relative to the root of targetFS to start scanning.
	// Since targetFS IS the directory, we scan ".".
	results, err := p.scanner.ScanFS(ctx, targetFS, ".")
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("trivy scan failed: %w", err)
	}

	violations := p.convertResultsToViolations(results)
	compliant := violations.isCompliant(p.cfg.SeverityThresholds)

	logr.Info("trivy policy evaluation complete",
		"total_violations", len(violations),
		"compliant", compliant)

	if compliant {
		return PolicyCheckResponse{
			Compliant:  compliant,
			Violations: []PolicyViolation{},
		}, nil
	}

	return PolicyCheckResponse{
		Compliant:  compliant,
		Violations: violations,
	}, nil
}

func (p TrivyPolicyChecker) convertResultsToViolations(results scan.Results) PolicyViolations {
	var violationList []PolicyViolation

	// Filter for failed results
	failedResults := results.GetFailed()

	for _, result := range failedResults {
		// Map severity
		severity := strings.ToLower(string(result.Severity()))

		// Map file path
		// result.Range().GetFilename() usually returns the path.
		// Since we scanned relative to req.IACSource, this might be a relative path.
		// We can leave it as relative or try to make it absolute if we had the base.
		// For now, we take what Trivy gives.
		filePath := result.Range().GetFilename()

		v := PolicyViolation{
			PolicyName:  result.Rule().Summary,
			Description: result.Description(),
			FilePath:    filePath,
			Severity:    severity,
		}

		// Use the rule ID or AVDID as PolicyName if Summary is empty
		if v.PolicyName == "" {
			v.PolicyName = result.Rule().AVDID
		}
		if v.PolicyName == "" {
			v.PolicyName = result.Rule().LongID()
		}

		violationList = append(violationList, v)
	}

	return violationList
}

// Ensure the interface is satisfied (if we decide to use the same interface as existing PolicyChecker,
// though currently PolicyChecker is a struct. If we refactored to an interface, this would implement it).
// For now, it matches the Tool interface via Declaration and Call.
