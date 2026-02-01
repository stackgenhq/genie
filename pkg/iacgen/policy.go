package iacgen

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/appcd-dev/go-lib/logger"
	"github.com/snyk/policy-engine/pkg/data"
	"github.com/snyk/policy-engine/pkg/engine"
	"github.com/snyk/policy-engine/pkg/input"
	"github.com/snyk/policy-engine/pkg/models"
	"github.com/snyk/policy-engine/pkg/postprocess"
	"github.com/spf13/afero"
)

type PolicyCheckRequest struct {
	IACSource string
	Policies  []string
}

type PolicyCheckResponse struct {
	Compliant  bool
	Violations PolicyViolations
}

type PolicyViolations []PolicyViolation

// NewViolations converts policy engine results to simplified PolicyViolation structs.
// Automatically deduplicates violations using a composite key of PolicyName|FilePath|Description.
func NewViolations(results []models.Result) []PolicyViolation {
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
}

func (p PolicyViolation) String() string {
	return fmt.Sprintf("%s: %s", p.PolicyName, p.Description)
}

type PolicyChecker interface {
	CheckPolicy(ctx context.Context, req PolicyCheckRequest) (PolicyCheckResponse, error)
}

func NewEmbeddedPolicyChecker() PolicyChecker {
	return policyChecker{}
}

type policyChecker struct{}

func (p policyChecker) downloadSyncPolicyBundle(ctx context.Context, destDir string) (*os.File, error) {
	// from https://downloads.snyk.io/cli/iac/rules/versions.json
	snykBundleURL := "https://static.snyk.io/cli/iac/rules/v0.31.9/bundle.tar.gz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snykBundleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for policy bundle: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download policy bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download policy bundle: %s", resp.Status)
	}

	policyBundleFile, err := os.CreateTemp(destDir, "policy-bundle-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for policy bundle: %w", err)
	}

	_, err = io.Copy(policyBundleFile, resp.Body)
	if err != nil {
		policyBundleFile.Close()
		return nil, fmt.Errorf("failed to save policy bundle to file: %w", err)
	}
	_, err = policyBundleFile.Seek(0, io.SeekStart)
	if err != nil {
		policyBundleFile.Close()
		return nil, fmt.Errorf("failed to seek to start of policy bundle file: %w", err)
	}
	return policyBundleFile, nil
}

func (p policyChecker) CheckPolicy(ctx context.Context, req PolicyCheckRequest) (PolicyCheckResponse, error) {
	tmpDir, err := os.MkdirTemp("", "iac-policy-check")
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	logr := logger.GetLogger(ctx)

	// 1. Download policy bundle
	policyBundleFile, err := p.downloadSyncPolicyBundle(ctx, tmpDir)
	if err != nil {
		return PolicyCheckResponse{}, err
	}
	defer policyBundleFile.Close()

	// 2. Load the policy bundle
	policyEngine := engine.NewEngine(ctx, &engine.EngineOptions{
		Providers: []data.Provider{
			data.TarGzProvider(policyBundleFile),
		},
		Timeouts: engine.Timeouts{
			Init:  10 * time.Minute,
			Eval:  10 * time.Minute,
			Query: 10 * time.Minute,
		},
	})
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("failed to create policy engine: %w", err)
	}

	// 3. Load IaC files
	loader := input.NewLoader(&input.TfDetector{})
	detectable := &input.Directory{
		Path: req.IACSource,
		Fs:   afero.NewOsFs(),
	}

	_, err = loader.Load(detectable, input.DetectOptions{})
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("failed to load IaC files: %w", err)
	}

	logr.Info("loaded files", "count", loader.Count())

	// 4. Evaluate the configuration against policies
	results := policyEngine.Eval(ctx, &engine.EvalOptions{
		Inputs: loader.ToStates(),
	})

	// 5. Add source locations to results (populates Resources[].Location[] with file paths)
	postprocess.AddSourceLocs(results, loader)

	violations := NewViolations(results.Results)

	logr.Info("policy evaluation complete", "unique_violations", len(violations))

	return PolicyCheckResponse{
		Compliant:  len(violations) == 0,
		Violations: violations,
	}, nil
}
