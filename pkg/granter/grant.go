package granter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appcd-dev/cce/pkg/cce"
	"github.com/appcd-dev/cce/pkg/dirutils"
	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/iacgen/generator"
	"github.com/appcd-dev/genie/pkg/tui"
	"github.com/appcd-dev/go-lib/encodeutils"
	"github.com/appcd-dev/go-lib/logger"
)

// Main logic about the what happens when grant is run
// We scan the repo and find all the method calls using CCE
// We then send that info the LLM to get back the resources of interest
// then we use terraform MCP to generate the terraform code based on the resources requested
// then we use stackgen policy compliances to ensure the generated code is compliant with best practices

func New(
	analyzer analyzer.Analyzer,
	architect generator.Architect,
	iacWriter generator.IACWriter,
) Granter {
	return Granter{
		analyzer:  analyzer,
		architect: architect,
		iacWriter: iacWriter,
	}
}

type Granter struct {
	analyzer  analyzer.Analyzer
	architect generator.Architect
	iacWriter generator.IACWriter
}

type GrantRequest struct {
	CodeDir   string
	Language  cce.Language
	SaveTo    string
	EventChan chan<- interface{}
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
	logr := logger.GetLogger(ctx).With("fn", "Granter.Generate")
	if err := req.validate(); err != nil {
		return GrantResponse{}, err
	}

	// Emit stage progress: Analyzing (stage 0 of 3)
	emitStageProgress(req.EventChan, "Probing", 0, 3)
	emitThinking(req.EventChan, "Code Prober", "Scanning your codebase...")

	response.AnalysisOutput, err = g.analyzeRepo(ctx, req)
	if err != nil {
		return GrantResponse{}, err
	}

	// Emit stage progress: Architecting (stage 1 of 3)
	emitStageProgress(req.EventChan, "Ideating", 1, 3)

	// Emit analysis statistics
	providerCounts := make(map[string]int)
	resourceCounts := make(map[string]int)
	for _, res := range response.AnalysisOutput {
		providerCounts[res.MappedResource.Provider]++
		resourceCounts[res.MappedResource.Resource]++
	}

	emitThinking(req.EventChan, "Architect", "Designing your infrastructure...")

	architectResponse, err := g.architect.Design(ctx, generator.DesignCloudRequest{
		MethodCalls: response.AnalysisOutput,
		SaveTo:      req.SaveTo,
		EventChan:   req.EventChan,
	})
	if err != nil {
		return response, err
	}
	logr.Info("got the notes from architect", "count", len(architectResponse.Notes))
	response.Notes = append(response.Notes, architectResponse.Notes...)

	// Emit stage progress: Building (stage 2 of 3)
	emitStageProgress(req.EventChan, "Building", 2, 3)
	emitThinking(req.EventChan, "IAC Writer", "Creating infrastructure code...")

	logr.Info("Calling IaC writer", "architectNotes", architectResponse.Notes, "outputFolder", req.SaveTo)
	iacResponse, err := g.iacWriter.CreateIAC(ctx, generator.IACRequest{
		ArchitectureRequirement: architectResponse.Notes,
		OutputFolder:            req.SaveTo,
		EventChan:               req.EventChan,
	})
	if err != nil {
		logr.Error("IaC writer failed", "error", err)
		return response, err
	}
	logr.Info("IaC writer completed", "iacCodePath", iacResponse.IACCodePath, "notesCount", len(iacResponse.Notes))

	// Check if files were actually created
	files, _ := os.ReadDir(req.SaveTo)
	tfFiles := []string{}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".tf") {
			tfFiles = append(tfFiles, f.Name())
		}
	}
	logr.Info("Terraform files in output folder", "tfFiles", tfFiles, "count", len(tfFiles))

	response.Notes = append(response.Notes, iacResponse.Notes...)
	logr.Info("IAC Files generated", "notes", iacResponse.Notes, "location", iacResponse.IACCodePath)
	return response, nil
}

// emitStageProgress emits a stage progress event to the event channel if it's provided
func emitStageProgress(eventChan chan<- interface{}, stage string, stageIndex, totalStages int) {
	if eventChan == nil {
		return
	}
	progress := float64(stageIndex) / float64(totalStages)
	select {
	case eventChan <- tui.StageProgressMsg{
		Stage:       stage,
		Progress:    progress,
		StageIndex:  stageIndex,
		TotalStages: totalStages,
	}:
	default:
		// Channel full, skip
	}
}

// emitThinking emits a thinking event to the event channel if it's provided
func emitThinking(eventChan chan<- interface{}, agentName, message string) {
	if eventChan == nil {
		return
	}
	select {
	case eventChan <- tui.AgentThinkingMsg{
		AgentName: agentName,
		Message:   message,
	}:
	default:
		// Channel full, skip
	}
}

func (g Granter) analyzeRepo(ctx context.Context, req GrantRequest) (mappedResource analyzer.MappedResources, err error) {
	logr := logger.GetLogger(ctx).With("fn", "Granter.analyzeRepo")
	logr.Debug("Analyzing the code directory", "codeDir", req.CodeDir)
	// create cce_analysis.ndjson file in the req.SaveTo directory
	analysisOutput, err := g.analyzer.Analyze(ctx, analyzer.AnalysisInput{
		Path:     req.CodeDir,
		Language: req.language(),
	})
	if err != nil {
		return analysisOutput, err
	}
	logr.Info("I know what you have. Let me design your infrastructure", "outputCount", len(analysisOutput))
	cceNDJSON, err := os.Create(filepath.Join(req.SaveTo, "cce_analysis.ndjson"))
	if err != nil {
		return analysisOutput, fmt.Errorf("error creating the cce analysis ndjson file: %w", err)
	}
	defer func() { _ = cceNDJSON.Close() }()
	for i := range analysisOutput {
		//
		_, err = fmt.Fprintf(cceNDJSON, "%s\n", encodeutils.MustToJSON(ctx, analysisOutput[i]))
		if err != nil {
			return analysisOutput, fmt.Errorf("error writing the analysis output: %w", err)
		}
	}
	return analysisOutput, err
}
