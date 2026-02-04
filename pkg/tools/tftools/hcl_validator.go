package tftools

import (
	"context"
	"fmt"

	"github.com/hashicorp/hcl/v2/hclparse"
)

type hclValidatorOutput struct {
	IsValid      bool     `json:"is_valid"`
	Notes        []string `json:"notes"`
	FilesChecked int      `json:"files_checked"`
}

func NewValidator() hclValidator {
	return hclValidator{}
}

type hclValidator struct{}

// Validate validates all .tf files in the given path (file or directory)
// If a directory is provided, it recursively validates all .tf files in subdirectories
func (v hclValidator) validate(ctx context.Context, req TFValidatorInput) (hclValidatorOutput, error) {
	if req.IACPath == "" {
		return hclValidatorOutput{}, fmt.Errorf("IACPath is required")
	}

	filesToValidate, err := req.filesOfInterest()
	if err != nil {
		return hclValidatorOutput{}, err
	}
	if len(filesToValidate) == 0 {
		return hclValidatorOutput{
			IsValid: true,
			Notes:   []string{"no .tf files found to validate"},
		}, nil
	}

	// Validate all found files
	return v.validateFiles(filesToValidate)
}

func (hclValidator) validateFiles(files []string) (hclValidatorOutput, error) {
	parser := hclparse.NewParser()
	result := hclValidatorOutput{
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
