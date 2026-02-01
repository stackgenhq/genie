package iacgen

import (
	"context"
	"fmt"
	"strings"

	"github.com/appcd-dev/go-lib/logger"
)

type GenerateIACRequest struct {
	AnalysisJSONFilePath string
	SaveTo               string
}

func (req GenerateIACRequest) validate() error {
	errors := []string{}
	if req.AnalysisJSONFilePath == "" {
		errors = append(errors, "path to CCE analysis JSON file is required")
	}
	if req.SaveTo == "" {
		errors = append(errors, "path to save the generated terraform code is required")
	}
	if len(errors) > 0 {
		return fmt.Errorf("validation errors: %s", strings.Join(errors, ", "))
	}
	return nil
}

type GenerateIACResponse struct {
	Notes []string
}

type IGenerator interface {
	GenerateIAC(ctx context.Context, req GenerateIACRequest) (GenerateIACResponse, error)
}

type Generator struct {
	iacFixer  Fixer
	iacWriter IACWriter
}

func NewGenerator(
	iacWriter IACWriter,
	iacFixer Fixer,
) Generator {
	return Generator{
		iacFixer:  iacFixer,
		iacWriter: iacWriter,
	}
}

func (g Generator) GenerateIAC(ctx context.Context, req GenerateIACRequest) (GenerateIACResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "GenerateIAC")
	if err := req.validate(); err != nil {
		return GenerateIACResponse{}, err
	}
	// Generate the IAC code from the given CCE NDJSON file
	iacWriterResult, err := g.iacWriter.WriteIAC(ctx, IACWriterInput{
		AnalysisNDJSONFilePath: req.AnalysisJSONFilePath,
		SaveTo:                 req.SaveTo,
	})
	if err != nil {
		return GenerateIACResponse{}, err
	}
	logr.Info("IAC code generated successfully", "iacFilePath", iacWriterResult.IACFilePath)
	_, err = g.iacFixer.FixIAC(ctx, FixIACRequest{
		IACFilePath: iacWriterResult.IACFilePath,
	})
	if err != nil {
		return GenerateIACResponse{}, err
	}
	logr.Info("IAC code fixed successfully", "iacFilePath", iacWriterResult.IACFilePath)

	// Placeholder implementation
	return GenerateIACResponse{
		Notes: iacWriterResult.Notes,
	}, nil

}
