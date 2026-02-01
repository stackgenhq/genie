package analyzer

import (
	"context"
	"fmt"

	"github.com/appcd-dev/go-lib/encodeutils"
	libmcp "github.com/appcd-dev/go-lib/mcp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sks/cce/pkg/analyzer"
	"github.com/sks/cce/pkg/analyzer/analyzercommon"
	"github.com/sks/cce/pkg/models"
	"github.com/sks/cce/pkg/resourcemapper"
)

type Analyzer interface {
	Analyze(ctx context.Context, input analyzercommon.AnalysisInput) (MappedResources, error)
}

var _ Analyzer = (*treeSitterBasedAnalyzer)(nil)

type MappedResource struct {
	MappedResource models.MappedResource `json:"mapped_resource"`
	MethodCall     models.MethodCall     `json:"method_call"`
}

type MappedResources []MappedResource

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

func (a treeSitterBasedAnalyzer) Analyze(ctx context.Context, input analyzercommon.AnalysisInput) (MappedResources, error) {
	scanResult, err := a.analyzer.AnalyzeV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze the folder: %w", err)
	}
	result := MappedResources{}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			methodCall, ok := <-scanResult.Channel
			if !ok {
				return result, nil
			}
			mappedResource, err := a.resourceMapper.Map(ctx, resourcemapper.MappingRequest{
				MethodName: methodCall.Name,
				Language:   input.Language,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to map method call: %w", err)
			}
			result = append(result, MappedResource{
				MappedResource: mappedResource,
				MethodCall:     methodCall,
			})
		}
	}
}

func (a treeSitterBasedAnalyzer) Tools(ctx context.Context) []libmcp.ToolDefinition {
	return []libmcp.ToolDefinition{
		{
			Tool: mcp.Tool{
				Name:           "code-analyzer",
				Description:    `Analyzes the given source code to extract method calls`,
				RawInputSchema: encodeutils.MustToJSON(ctx, libmcp.GetSchema(&analyzercommon.AnalysisInput{})),
			},
			Handler: a.analyze,
		},
	}
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
		Content: make([]mcp.Content, 0, len(analysisResponse.MethodCalls)),
	}
	for i := range analysisResponse.MethodCalls {
		result.Content[i] = mcp.NewTextContent(string(encodeutils.MustToJSON(ctx, analysisResponse.MethodCalls[i])))
	}
	return result, nil
}
