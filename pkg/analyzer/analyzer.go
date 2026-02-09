package analyzer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/appcd-dev/cce/pkg/analyzer"
	"github.com/appcd-dev/cce/pkg/cce"
	"github.com/appcd-dev/cce/pkg/models"
	"github.com/appcd-dev/cce/pkg/resourcemapper"
	"github.com/appcd-dev/go-lib/encodeutils"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"
)

type AnalysisInput struct {
	Path     string
	Language cce.Language

	// SaveCCEJSONTo is the path to save the CCE JSON to
	SaveCCEJSONTo string
}

//go:generate go tool counterfeiter -generate

//counterfeiter:generate  --fake-name FakeCCEAnalyzer github.com/appcd-dev/cce/pkg/analyzer.Analyzer

//counterfeiter:generate . Analyzer
type Analyzer interface {
	Analyze(ctx context.Context, input AnalysisInput) (MappedResources, error)
	Tool() server.ServerTool
}

var _ Analyzer = (*TreeSitterBasedAnalyzer)(nil)

type MappedResource struct {
	MappedResource models.MappedResource `json:"mapped_resource"`
	MethodCall     models.MethodCall     `json:"method_call"`
}

func (m MappedResource) String() string {
	var sb strings.Builder

	// Main resource information
	sb.WriteString(fmt.Sprintf("%s resource %s referenced in method %s\n",
		m.MappedResource.Provider,
		m.MappedResource.Resource,
		m.MethodCall.Name))

	// File location
	sb.WriteString(fmt.Sprintf("  Location: %s:%d:%d\n",
		m.MethodCall.FilePath,
		m.MethodCall.Line,
		m.MethodCall.Column))

	// Parent function context
	if m.MethodCall.ParentFunction != "" {
		contextType := m.MethodCall.ParentContext
		if contextType == "" {
			contextType = "function"
		}
		sb.WriteString(fmt.Sprintf("  Inside %s: %s\n", contextType, m.MethodCall.ParentFunction))
	}

	// Code snippet for context
	if m.MethodCall.CodeSnippet != "" {
		sb.WriteString("  Code context:\n")
		// Indent each line of the code snippet
		lines := strings.Split(m.MethodCall.CodeSnippet, "\n")
		for _, line := range lines {
			sb.WriteString("    ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

type MappedResources []MappedResource

type GroupdResources map[string]MappedResources

func (m MappedResources) GroupByProvider() GroupdResources {
	groupedResources := make(GroupdResources)
	for _, resource := range m {
		groupedResources[resource.MappedResource.Provider] = append(groupedResources[resource.MappedResource.Provider], resource)
	}
	return groupedResources
}

func (m MappedResources) GroupByResources() GroupdResources {
	groupedResources := make(GroupdResources)
	for _, resource := range m {
		groupedResources[resource.MappedResource.Resource] = append(groupedResources[resource.MappedResource.Resource], resource)
	}
	return groupedResources
}

func New(ctx context.Context, mappingDefinitionFile string, cceAnalyzer analyzer.Analyzer) (Analyzer, error) {
	resourceMapper, err := resourcemapper.NewFileBasedMapper(ctx, mappingDefinitionFile)
	return TreeSitterBasedAnalyzer{
		analyzer:       cceAnalyzer,
		resourceMapper: resourceMapper,
	}, err
}

type TreeSitterBasedAnalyzer struct {
	analyzer       analyzer.Analyzer
	resourceMapper resourcemapper.Mapper
}

func (a TreeSitterBasedAnalyzer) Tool() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("analyze_infrastructure",
			mcp.WithDescription("Analyzes source code to identify and extract cloud infrastructure resource usages (like S3 buckets, Lambda functions) and their context."),
			mcp.WithString("path",
				mcp.Required(),
				mcp.Description("Absolute path to the source code directory to be analyzed"),
			),
			mcp.WithString("save_to",
				mcp.Required(),
				mcp.Description("Absolute path to a directory where the analysis results (CCE JSON) will be saved. The directory must exist"),
			),
		),
		Handler: a.analyzeToolCall,
	}
}

func (a TreeSitterBasedAnalyzer) analyzeToolCall(ctx context.Context, request mcp.CallToolRequest) (_ *mcp.CallToolResult, err error) {
	input := AnalysisInput{}
	missingFields := make([]string, 0, 2)
	input.Path, err = request.RequireString("path")
	if err != nil {
		missingFields = append(missingFields, "path")
	}

	input.SaveCCEJSONTo, err = request.RequireString("save_to")
	if err != nil {
		missingFields = append(missingFields, "save_to")
	}

	if len(missingFields) > 0 {
		return nil, fmt.Errorf("missing required fields: %v", missingFields)
	}

	cceNDJSONFile := filepath.Join(input.SaveCCEJSONTo, "cce_analysis.ndjson")
	analysisOutput, err := a.Analyze(ctx, AnalysisInput{
		Path:          input.Path,
		SaveCCEJSONTo: cceNDJSONFile,
	})
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}

	content := []mcp.Content{
		mcp.NewTextContent(fmt.Sprintf("Code context analysis JSON is saved to %s", cceNDJSONFile)),
	}

	for _, cloudBasedPrompt := range analysisOutput.Summarize(ctx) {
		content = append(content, mcp.NewTextContent(fmt.Sprintf("for cloud %s\n%s", cloudBasedPrompt.CloudProvider, string(encodeutils.MustToJSON(ctx, cloudBasedPrompt)))))
	}

	// return the result as JSON
	return &mcp.CallToolResult{
		Content: content,
	}, nil
}

func (a TreeSitterBasedAnalyzer) Analyze(ctx context.Context, input AnalysisInput) (result MappedResources, err error) {
	result = make(MappedResources, 0)
	logr := logger.GetLogger(ctx).With("fn", "TreeSitterBasedAnalyzer.Analyze")
	logr.Info("Analyzing folder", "folder", input.Path)

	// Verify that the path exists
	_, err = os.Stat(input.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to access analysis path: %w", err)
	}

	scanResult := make(chan models.MethodCall)
	defer func(startTime time.Time) {
		logr.Info("analysis completed", "duration", time.Since(startTime).String())
	}(time.Now())
	var writer *os.File
	if input.SaveCCEJSONTo != "" {
		writer, err = os.Create(input.SaveCCEJSONTo)
		if err != nil {
			return nil, fmt.Errorf("failed to create output file: %w", err)
		}
		defer func() {
			if closeErr := writer.Close(); closeErr != nil {
				logr.Warn("failed to close output file", "error", closeErr)
			}
		}()
	}
	errGoup, ectx := errgroup.WithContext(ctx)
	errGoup.Go(func() error {
		return a.analyzer.AnalyzeV3(ectx, input.Path, scanResult)
	})
	errGoup.Go(func() error {
		for methodCall := range scanResult {
			logr.Debug("method call", "methodCall", methodCall)
			mappedResource, err := a.resourceMapper.Map(ectx, resourcemapper.MappingRequest{
				MethodName: methodCall.Name,
				Language:   methodCall.Language,
			})
			if err != nil {
				logr.Debug("could not map the method call", "methodCall", methodCall, "error", err)
				continue
			}
			resource := MappedResource{
				MappedResource: mappedResource,
				MethodCall:     methodCall,
			}
			if writer != nil {
				// ndjson file
				jsonBytes := encodeutils.MustToJSON(ectx, resource)
				_, _ = fmt.Fprintf(writer, "%s\n", jsonBytes)
			}
			result = append(result, resource)
		}
		return nil
	})
	err = errGoup.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze the folder: %w", err)
	}
	return result, nil
}
