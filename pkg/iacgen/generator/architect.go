package generator

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider"
	"github.com/appcd-dev/genie/pkg/langfuse"
	"github.com/appcd-dev/go-lib/logger"
	"golang.org/x/sync/errgroup"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:embed prompts/architect_persona.txt
var architectPersonaPrompt string

//go:embed prompts/readme_task.txt
var readmeTaskPrompt string

//counterfeiter:generate . Architect
type Architect interface {
	Design(ctx context.Context, req DesignCloudRequest) (DesignCloudResponse, error)
	Tool() tool.Tool
}

// ArchitectToolRequest is the input schema for the generate_architecture tool.
type ArchitectToolRequest struct {
	CCEFilePath string `json:"cce_file_path" jsonschema:"description=Absolute path to the CCE analysis result file (NDJSON),required"`
	SaveTo      string `json:"save_to" jsonschema:"description=Absolute path to the directory where architecture notes will be saved,required"`
}

// ArchitectToolResponse is the output of the generate_architecture tool.
type ArchitectToolResponse struct {
	Notes  string `json:"notes"`
	Status string `json:"status"`
}

type DesignCloudRequest struct {
	MethodCalls analyzer.MappedResources
	SaveTo      string
	EventChan   chan<- interface{}
}

type DesignCloudResponse struct {
	Notes []string
}

func NewLLMBasedArchitect(
	ctx context.Context,
	modelProvider modelprovider.ModelProvider,
	auditor audit.Auditor,
	toolsList ...tool.Tool,
) (Architect, error) {
	expertBio := expert.ExpertBio{
		Personality: langfuse.GetPrompt(ctx, "genie_architect_persona", architectPersonaPrompt),
		Name:        "software-architect",
		Description: "Software Architect",
		Tools:       toolsList,
	}
	expert, err := expertBio.ToExpert(ctx, modelProvider, auditor)
	if err != nil {
		return nil, err
	}

	return llmBasedArchitect{
		expert: expert,
	}, nil
}

type llmBasedArchitect struct {
	expert expert.Expert
}

func (a llmBasedArchitect) Tool() tool.Tool {
	return function.NewFunctionTool(
		a.executeTool,
		function.WithName("generate_architecture"),
		function.WithDescription(
			"Generates architectural insights and design patterns based on code analysis (CCE). "+
				"Reads a CCE analysis NDJSON file, determines cloud resources used by the codebase, "+
				"and produces architecture notes describing the infrastructure design."),
	)
}

func (a llmBasedArchitect) executeTool(ctx context.Context, req ArchitectToolRequest) (ArchitectToolResponse, error) {
	designReq := DesignCloudRequest{
		SaveTo: req.SaveTo,
	}
	var err error
	designReq.MethodCalls, err = a.mappedResources(ctx, req.CCEFilePath)
	if err != nil {
		return ArchitectToolResponse{Status: "error", Notes: err.Error()}, nil
	}

	resp, err := a.Design(ctx, designReq)
	if err != nil {
		return ArchitectToolResponse{Status: "error", Notes: err.Error()}, nil
	}

	return ArchitectToolResponse{
		Notes:  strings.Join(resp.Notes, "\n\n"),
		Status: "success",
	}, nil
}

func (llmBasedArchitect) mappedResources(ctx context.Context, cceNDJSONFilePath string) (analyzer.MappedResources, error) {
	file, err := os.Open(cceNDJSONFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CCE file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.GetLogger(ctx).Warn("failed to close CCE file", "error", closeErr)
		}
	}()

	var methodCalls analyzer.MappedResources
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var resource analyzer.MappedResource
		if err := json.Unmarshal(scanner.Bytes(), &resource); err != nil {
			logger.GetLogger(ctx).Warn("failed to unmarshal CCE resource, skipping line", "error", err)
			continue // Skip invalid lines
		}
		methodCalls = append(methodCalls, resource)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading CCE file: %w", err)
	}
	return methodCalls, nil
}

func (a llmBasedArchitect) Design(ctx context.Context, req DesignCloudRequest) (DesignCloudResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedArchitect.Design")
	logr.Info("creating architecture design")
	defer func(startTime time.Time) {
		logr.Info("architecture design phase complete", "duration", time.Since(startTime).String())
	}(time.Now())
	cloudBasedPrompts := req.MethodCalls.Summarize(ctx)
	if len(cloudBasedPrompts) == 0 {
		return DesignCloudResponse{
			Notes: []string{
				"There are no resources to generate IAC for.",
			},
		}, nil
	}
	multiCloudNotes := sync.Map{}
	errGroup, gctx := errgroup.WithContext(ctx)
	for _, cloudBasedPrompt := range cloudBasedPrompts {
		errGroup.Go(func() error {
			logr.Info("Generating architecture design for cloud provider", "cloudProvider", cloudBasedPrompt.CloudProvider, "identifiedResources", len(cloudBasedPrompt.Prompt))
			emitArchitectThinking(req.EventChan, fmt.Sprintf("Designing %s infrastructure...", cloudBasedPrompt.CloudProvider))
			notes, err := a.generateArchitecture(gctx, cloudBasedPrompt, req.EventChan)
			if err != nil {
				return err
			}
			logr.Info("architecture design phase complete", "cloudProvider", cloudBasedPrompt.CloudProvider)
			multiCloudNotes.Store(cloudBasedPrompt.CloudProvider, notes)
			return nil
		})
	}
	err := errGroup.Wait()
	if err != nil {
		return DesignCloudResponse{}, err
	}
	result := DesignCloudResponse{}
	multiCloudNotes.Range(func(key, value interface{}) bool {
		result.Notes = append(result.Notes, value.(string))
		return true
	})
	if err = a.saveNotes(ctx, req.SaveTo, result.Notes); err != nil {
		logr.Warn("error saving the architect notes", "error", err)
	}

	// Generate README.md after notes are completed (synchronous to prevent data loss on early exit)
	logr.Info("Generating README.md", "saveTo", req.SaveTo)
	emitArchitectThinking(req.EventChan, "Writing README.md...")
	if readmeErr := a.generateReadme(ctx, req.SaveTo, result); readmeErr != nil {
		logr.Warn("Failed to generate README.md", "error", readmeErr)
	}
	return result, nil
}

func (a llmBasedArchitect) saveNotes(ctx context.Context, saveTo string, notes []string) error {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedArchitect.saveNotes")
	logr.Info("Saving notes", "saveTo", saveTo)
	return os.WriteFile(filepath.Join(saveTo, "Architecture.md"), []byte(strings.Join(notes, "\n")), 0644)
}

func (a llmBasedArchitect) generateArchitecture(ctx context.Context, cloudInfraDetails analyzer.CloudBasedPrompt, eventChan chan<- interface{}) (string, error) {
	// Get the resource categorizer instance
	result, err := a.expert.Do(ctx, expert.Request{
		Message:      cloudInfraDetails.Prompt,
		EventChannel: eventChan,
		TaskType:     modelprovider.TaskNovelReasoning,
		Mode:         expert.CostOptimizedConfig(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get a plan from architect: %w", err)
	}
	notes := strings.Builder{}
	for i := range result.Choices {
		notes.WriteString(result.Choices[i].Message.Content)
	}
	return notes.String(), nil
}

// generateReadme creates a comprehensive README.md file in the SaveTo directory using LLM
// The README is designed to be technically sound, context-aware, and understandable by junior developers
func (a llmBasedArchitect) generateReadme(ctx context.Context, saveTo string, response DesignCloudResponse) error {
	logr := logger.GetLogger(ctx).With("fn", "llmBasedArchitect.generateReadme")

	// Ensure the directory exists
	if err := os.MkdirAll(saveTo, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", saveTo, err)
	}

	// Build a prompt for the LLM to generate a context-aware README
	prompt := langfuse.GetPrompt(ctx, "architect_readme", buildReadmePrompt(response.Notes))

	// Use the expert to generate the README content
	logr.Info("Generating README.md using LLM", "saveTo", saveTo)
	var readmeContent strings.Builder
	_, err := a.expert.Do(ctx, expert.Request{
		Message:  prompt,
		TaskType: modelprovider.TaskPlanning,
		ChoiceProcessor: func(choices ...model.Choice) {
			for _, choice := range choices {
				readmeContent.WriteString(choice.Message.Content)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("failed to generate README content: %w", err)
	}

	// Write README to file
	readmePath := filepath.Join(saveTo, "README.md")
	if err := os.WriteFile(readmePath, []byte(readmeContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write README.md: %w", err)
	}

	logr.Info("README.md created successfully", "path", readmePath)
	return nil
}

// buildReadmePrompt creates a prompt for generating a context-aware README.
// It uses the embedded readmeTaskPrompt template and injects architecture notes and a timestamp.
// Without this function, the README generation would lack architecture-specific context.
func buildReadmePrompt(notes []string) string {
	var recs strings.Builder
	for i, note := range notes {
		recs.WriteString(fmt.Sprintf("### Cloud Provider %d:\n", i+1))
		recs.WriteString(note)
		recs.WriteString("\n\n")
	}

	r := strings.NewReplacer(
		"{architecture_recommendations}", recs.String(),
		"{timestamp}", time.Now().Format("2006-01-02 15:04:05 MST"),
	)
	return r.Replace(readmeTaskPrompt)
}

// emitArchitectThinking sends an AgentThinkingMsg to the TUI event channel.
// It is a nil-safe no-op when the event channel is not provided.
func emitArchitectThinking(eventChan chan<- interface{}, message string) {
	if eventChan == nil {
		return
	}
	agui.EmitThinking(eventChan, "Architect", message)
}
