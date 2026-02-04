package analyzer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/appcd-dev/cce/pkg/analyzer"
	"github.com/appcd-dev/cce/pkg/analyzer/analyzercommon"
	"github.com/appcd-dev/cce/pkg/cce"
	"github.com/appcd-dev/cce/pkg/models"
	"github.com/appcd-dev/cce/pkg/resourcemapper"
	"github.com/appcd-dev/go-lib/encodeutils"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"golang.org/x/sync/errgroup"
)

type AnalysisInput struct {
	Path     string
	Language cce.Language
}

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . Analyzer
type Analyzer interface {
	Analyze(ctx context.Context, input AnalysisInput) (MappedResources, error)
}

var _ Analyzer = (*treeSitterBasedAnalyzer)(nil)

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

func New() (Analyzer, error) {
	resourceMapper, err := resourcemapper.NewInBuildMapping()
	return treeSitterBasedAnalyzer{
		analyzer:       analyzer.New(),
		resourceMapper: resourceMapper,
	}, err
}

type treeSitterBasedAnalyzer struct {
	analyzer       analyzer.Analyzer
	resourceMapper resourcemapper.Mapper
}

func (a treeSitterBasedAnalyzer) Analyze(ctx context.Context, input AnalysisInput) (MappedResources, error) {
	logr := logger.GetLogger(ctx).With("fn", "treeSitterBasedAnalyzer.Analyze")
	logr.Info("Analyzing folder", "folder", input.Path)
	scanResult := make(chan models.MethodCall)
	defer func(startTime time.Time) {
		logr.Info("analysis completed", "duration", time.Since(startTime).String())
	}(time.Now())
	result := MappedResources{}
	errGoup, ctx := errgroup.WithContext(ctx)
	errGoup.Go(func() error {
		return a.analyzer.AnalyzeV3(ctx, input.Path, scanResult)
	})
	errGoup.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case methodCall, ok := <-scanResult:
				if !ok {
					return nil
				}
				logr.Debug("method call", "methodCall", methodCall)
				mappedResource, err := a.resourceMapper.Map(ctx, resourcemapper.MappingRequest{
					MethodName: methodCall.Name,
					Language:   methodCall.Language,
				})
				if err != nil {
					logr.Debug("could not map the method call", "methodCall", methodCall, "error", err)
					continue
				}
				result = append(result, MappedResource{
					MappedResource: mappedResource,
					MethodCall:     methodCall,
				})
			}
		}
	})
	err := errGoup.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze the folder: %w", err)
	}
	return result, nil
}

func (a treeSitterBasedAnalyzer) analyze(ctx context.Context, req mcp.CallToolRequest) (_ *mcp.CallToolResult, err error) {
	var input analyzercommon.AnalysisInput
	err = encodeutils.Convert(req.Params, &input)
	if err != nil {
		return nil, err
	}

	analysisResponse, err := a.analyzer.Analyze(ctx, input)
	if err != nil {
		return nil, err
	}
	result := &mcp.CallToolResult{
		Content: make([]mcp.Content, len(analysisResponse.MethodCalls)),
	}
	for i := range analysisResponse.MethodCalls {
		result.Content[i] = mcp.NewTextContent(string(encodeutils.MustToJSON(ctx, analysisResponse.MethodCalls[i])))
	}
	return result, nil
}
