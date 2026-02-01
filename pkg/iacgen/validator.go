package iacgen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
)

type IACValidator interface {
	Validate(ctx context.Context, req IACValidatorInput) (IACValidatorOutput, error)
}

type IACValidatorInput struct {
	IACFilePath string // Can be a file or directory
}

type IACValidatorOutput struct {
	IsValid      bool
	Notes        []string
	FilesChecked int
}

func NewValidator() IACValidator {
	return &Validator{}
}

func (req IACValidatorInput) filesOfInterest() ([]string, error) {
	// Check if path exists
	info, err := os.Stat(req.IACFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to access path: %w", err)
	}

	var filesToValidate []string

	// If it's a directory, find all .tf files recursively
	if !info.IsDir() {
		// Single file - check if it's a .tf file
		if !strings.HasSuffix(req.IACFilePath, ".tf") {
			return nil, fmt.Errorf("file %s is not a .tf file", req.IACFilePath)
		}
		return []string{req.IACFilePath}, nil
	}
	err = filepath.Walk(req.IACFilePath, func(path string, info os.FileInfo, err error) error {
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

type Validator struct{}

// Validate validates all .tf files in the given path (file or directory)
// If a directory is provided, it recursively validates all .tf files in subdirectories
func (v *Validator) Validate(ctx context.Context, req IACValidatorInput) (IACValidatorOutput, error) {
	if req.IACFilePath == "" {
		return IACValidatorOutput{}, fmt.Errorf("IACFilePath is required")
	}

	filesToValidate, err := req.filesOfInterest()
	if err != nil {
		return IACValidatorOutput{}, err
	}
	if len(filesToValidate) == 0 {
		return IACValidatorOutput{
			IsValid: true,
			Notes:   []string{"no .tf files found to validate"},
		}, nil
	}

	// Validate all found files
	return v.validateFiles(filesToValidate)
}

func (v *Validator) validateFiles(files []string) (IACValidatorOutput, error) {
	parser := hclparse.NewParser()
	result := IACValidatorOutput{
		IsValid:      true,
		FilesChecked: len(files),
	}

	for _, filePath := range files {
		// Parse the HCL file
		_, diags := parser.ParseHCLFile(filePath)

		if diags.HasErrors() {
			result.IsValid = false
			for _, d := range diags {
				result.Notes = append(result.Notes, fmt.Sprintf("[%s] %s", filePath, d.Error()))
			}
		}
	}

	if !result.IsValid {
		result.Notes = append([]string{
			fmt.Sprintf("✗ Validation failed for some files (checked %d file(s))", result.FilesChecked),
		}, result.Notes...)
	}

	return result, nil
}
