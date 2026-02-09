package tftools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appcd-dev/go-lib/encodeutils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TFValidatorInput represents the input for Terraform validation
type TFValidatorInput struct {
	IACPath string `json:"iac_path"`
}

func (req TFValidatorInput) filesOfInterest() ([]string, error) {
	// Check if path exists
	info, err := os.Stat(req.IACPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}

	var filesToValidate []string

	// If it's a directory, find all .tf files recursively
	if !info.IsDir() {
		// Single file - check if it's a .tf file
		if !strings.HasSuffix(req.IACPath, ".tf") {
			return nil, fmt.Errorf("file %s is not a .tf file", req.IACPath)
		}
		return []string{req.IACPath}, nil
	}
	err = filepath.Walk(req.IACPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk directory: %w", err)
		}
		// Check if it's a .tf file
		if !info.IsDir() && filepath.Ext(path) == ".tf" {
			filesToValidate = append(filesToValidate, path)
		}
		return nil
	})
	return filesToValidate, err
}

type tfValidationResult struct {
	IsValid      bool                  `json:"is_valid"`
	TFExecResult tfExecValidatorOutput `json:"tfexec_result"`
	HCLResult    hclValidatorOutput    `json:"hcl_result"`
}

// This one can do HCL and tfexec validation in parallel
type TFValidator struct {
}

func (t TFValidator) Tool() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("validate_iac",
			mcp.WithDescription("Validates Terraform/OpenTofu configurations"),
			mcp.WithString("iac_path",
				mcp.Required(),
				mcp.Description("Absolute path to the directory containing Terraform/OpenTofu .tf files"),
			),
		),
		Handler: t.validateToolCall,
	}
}

func (t TFValidator) validateToolCall(ctx context.Context, request mcp.CallToolRequest) (_ *mcp.CallToolResult, err error) {
	input := TFValidatorInput{}
	missingFields := make([]string, 0, 2)
	input.IACPath, err = request.RequireString("iac_path")
	if err != nil {
		missingFields = append(missingFields, "iac_path")
	}

	if len(missingFields) > 0 {
		return nil, fmt.Errorf("missing required fields: %v", missingFields)
	}
	result, err := t.validate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(string(encodeutils.MustToJSON(ctx, result))),
		},
	}, nil
}

func (t TFValidator) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "validate_iac",
		Description: "Validates Terraform/OpenTofu configurations",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"iac_path": {
					Type:        "string",
					Description: "Absolute path to the directory containing Terraform/OpenTofu .tf files",
				},
			},
			Required: []string{"iac_path"},
		},
	}
}

func (t TFValidator) Call(ctx context.Context, jsonArgs []byte) (_ any, err error) {
	var input TFValidatorInput
	if err := json.Unmarshal(jsonArgs, &input); err != nil {
		return nil, err
	}
	return t.validate(ctx, input)
}

func (t TFValidator) validate(ctx context.Context, input TFValidatorInput) (_ tfValidationResult, err error) {
	if input.IACPath == "" {
		return tfValidationResult{}, fmt.Errorf("iac_path cannot be empty")
	}
	input.IACPath, err = filepath.Abs(input.IACPath)
	if err != nil {
		return tfValidationResult{}, fmt.Errorf("failed to get absolute path: %w", err)
	}
	result := tfValidationResult{}
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() (err error) {
		execValidator := tfExecValidator{}
		if _, _, err = execValidator.detectBinary(); err == nil {
			result.TFExecResult, err = execValidator.validate(ctx, input)
		}
		return err
	})
	errGroup.Go(func() (err error) {
		hclValidator := hclValidator{}
		result.HCLResult, err = hclValidator.validate(ctx, input)
		return err
	})
	err = errGroup.Wait()
	result.IsValid = result.TFExecResult.IsValid && result.HCLResult.IsValid
	if result.IsValid {
		return tfValidationResult{
			IsValid: true,
		}, nil
	}
	return result, err
}
