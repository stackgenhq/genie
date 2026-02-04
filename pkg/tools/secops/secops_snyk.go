package secops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/appcd-dev/go-lib/logger"
	"github.com/snyk/policy-engine/pkg/data"
	"github.com/snyk/policy-engine/pkg/engine"
	"github.com/snyk/policy-engine/pkg/input"
	"github.com/snyk/policy-engine/pkg/postprocess"
	"github.com/spf13/afero"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func newSnykPolicyChecker(ctx context.Context, ops SecOpsConfig) (tool.Tool, error) {
	checker := SnykPolicyChecker{
		cfg: ops,
	}
	var err error
	checker.bundleFile, err = checker.downloadSyncPolicyBundle(ctx)
	return checker, err
}

type SnykPolicyChecker struct {
	bundleFile string
	cfg        SecOpsConfig
}

func (p SnykPolicyChecker) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "check_iac_policy",
		Description: "Check Terraform/OpenTofu code against security policies to identify compliance violations",
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

func (p SnykPolicyChecker) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var req PolicyCheckRequest
	if err := json.Unmarshal(jsonArgs, &req); err != nil {
		return nil, err
	}
	return p.CheckPolicy(ctx, req)
}

func (p SnykPolicyChecker) downloadSyncPolicyBundle(ctx context.Context) (string, error) {
	// from https://downloads.snyk.io/cli/iac/rules/versions.json
	snykBundleURL := "https://static.snyk.io/cli/iac/rules/v0.31.9/bundle.tar.gz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snykBundleURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for policy bundle: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download policy bundle: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download policy bundle: %s", resp.Status)
	}

	policyBundleFile, err := os.CreateTemp(os.TempDir(), "policy-bundle-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for policy bundle: %w", err)
	}
	defer func() { _ = policyBundleFile.Close() }()

	_, err = io.Copy(policyBundleFile, resp.Body)
	if err != nil {
		_ = policyBundleFile.Close()
		return "", fmt.Errorf("failed to save policy bundle to file: %w", err)
	}
	_, err = policyBundleFile.Seek(0, io.SeekStart)
	if err != nil {
		_ = policyBundleFile.Close()
		return "", fmt.Errorf("failed to seek to start of policy bundle file: %w", err)
	}
	return policyBundleFile.Name(), nil
}

func (p SnykPolicyChecker) CheckPolicy(ctx context.Context, req PolicyCheckRequest) (PolicyCheckResponse, error) {
	tmpDir, err := os.MkdirTemp("", "iac-policy-check")
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logr := logger.GetLogger(ctx)

	// 1. Download policy bundle
	policyBundleFile, err := os.Open(p.bundleFile)
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("could not open the policy file: %w", err)
	}
	defer func() { _ = policyBundleFile.Close() }()

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

	// 3. Load IaC files
	loader := input.NewLoader(&input.TfDetector{})
	detectable := &input.Directory{
		Path: req.IACSource,
		Fs:   afero.NewOsFs(),
	}

	_, err = loader.Load(detectable, input.DetectOptions{})
	if err != nil {
		return PolicyCheckResponse{}, fmt.Errorf("there was an error loading the IAC file: %w", err)
	}
	// 4. Evaluate the configuration against policies
	results := policyEngine.Eval(ctx, &engine.EvalOptions{
		Inputs: loader.ToStates(),
	})

	// 5. Add source locations to results (populates Resources[].Location[] with file paths)
	postprocess.AddSourceLocs(results, loader)

	violations := NewViolations(results.Results)

	compliant := violations.isCompliant(p.cfg.SeverityThresholds)

	logr.Info("policy evaluation complete",
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
