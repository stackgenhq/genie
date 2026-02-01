package granter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/iacgen"
	"github.com/appcd-dev/go-lib/encodeutils"
	"github.com/sks/cce/pkg/analyzer/analyzercommon"
	"github.com/sks/cce/pkg/cce"
	"github.com/sks/cce/pkg/dirutils"
)

// Main logic about the what happens when grant is run
// We scan the repo and find all the method calls using CCE
// We then send that info the LLM to get back the resources of interest
// then we use terraform MCP to generate the terraform code based on the resources requested
// then we use stackgen policy compliances to ensure the generated code is compliant with best practices

func New(
	analyzer analyzer.Analyzer,
	iacGenerator iacgen.Generator,
) Granter {
	return Granter{
		analyzer:     analyzer,
		iacGenerator: iacGenerator,
	}
}

type Granter struct {
	analyzer     analyzer.Analyzer
	iacGenerator iacgen.Generator
}

type GrantRequest struct {
	CodeDir  string
	Language cce.Language
	SaveTo   string
}

func (r GrantRequest) language() cce.Language {
	if r.Language != cce.LanguageUNSPECIFIED {
		return r.Language
	}
	lang, err := dirutils.GetLanguageForDir(r.CodeDir)
	if err != nil {
		return cce.LanguageUNSPECIFIED
	}
	return lang
}

func (r GrantRequest) validate() error {
	errors := []string{}
	if lang := r.language(); lang == cce.LanguageUNSPECIFIED {
		errors = append(errors, "could not determine programming language of the code directory")
	}
	if r.CodeDir == "" {
		errors = append(errors, "code directory is required")
	}
	if r.SaveTo == "" {
		errors = append(errors, "path to save the generated terraform code is required")
	}
	if len(errors) > 0 {
		return fmt.Errorf("validation errors: %s", strings.Join(errors, ", "))
	}
	return nil
}

type GrantResponse struct {
	CCEAnalysisFilePath string
	AnalysisOutput      analyzer.MappedResources
	Notes               []string
}

func (g Granter) Generate(ctx context.Context, req GrantRequest) (response GrantResponse, err error) {
	if err := req.validate(); err != nil {
		return GrantResponse{}, err
	}
	response.AnalysisOutput, response.CCEAnalysisFilePath, err = g.analyzeRepo(ctx, req)
	if err != nil {
		return GrantResponse{}, err
	}
	// use this to create the IAC
	generator := iacgen.NewGenerator(
		iacgen.NewIACWriter(),
		iacgen.NewFixer(iacgen.NewEmbeddedPolicyChecker(), iacgen.NewValidator()),
	)
	result, err := generator.GenerateIAC(ctx, iacgen.GenerateIACRequest{
		AnalysisJSONFilePath: response.CCEAnalysisFilePath,
		SaveTo:               req.SaveTo,
	})
	if err != nil {
		return response, err
	}
	response.Notes = append(response.Notes, result.Notes...)
	return response, nil
}

func (g Granter) analyzeRepo(ctx context.Context, req GrantRequest) (mappedResource analyzer.MappedResources, cceYAMLPathstring string, err error) {
	// create cce_analysis.ndjson file in the req.SaveTo directory
	analysisOutput, err := g.analyzer.Analyze(ctx, analyzercommon.AnalysisInput{
		File:     req.CodeDir,
		Language: req.language(),
	})
	if err != nil {
		return analysisOutput, "", err
	}
	cceNDJSON, err := os.Create(filepath.Join(req.SaveTo, "cce_analysis.ndjson"))
	if err != nil {
		return analysisOutput, "", fmt.Errorf("error creating the cce analysis ndjson file: %w", err)
	}
	defer cceNDJSON.Close()
	for i := range analysisOutput {
		//
		_, err = cceNDJSON.Write(encodeutils.MustToJSON(ctx, analysisOutput[i]))
		if err != nil {
			return analysisOutput, "", fmt.Errorf("error writing the analysis output: %w", err)
		}
	}
	return analysisOutput, cceNDJSON.Name(), err
}
