package analyzer

import (
	"context"
	"fmt"

	"github.com/appcd-dev/cce/pkg/analyzer/analyzercommon"
	"github.com/appcd-dev/cce/pkg/models"
	"github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockAnalyzer implements analyzer.Analyzer interface from cce
type mockAnalyzer struct {
	analyzeFunc func(context.Context, analyzercommon.AnalysisInput) (analyzercommon.AnalysisResult, error)
}

func (m *mockAnalyzer) Analyze(ctx context.Context, input analyzercommon.AnalysisInput) (analyzercommon.AnalysisResult, error) {
	if m.analyzeFunc != nil {
		return m.analyzeFunc(ctx, input)
	}
	return analyzercommon.AnalysisResult{}, nil
}

func (m *mockAnalyzer) AnalyzeV2(ctx context.Context, input analyzercommon.AnalysisInput) (analyzercommon.AnalysisStreamingResponse, error) {
	return analyzercommon.AnalysisStreamingResponse{}, nil
}

func (m *mockAnalyzer) AnalyzeV3(ctx context.Context, folder string, result chan<- models.MethodCall) error {
	return nil
}

var _ = Describe("Analyzer Internal", func() {
	Describe("analyze", func() {
		var (
			tsAnalyzer treeSitterBasedAnalyzer
			mock       *mockAnalyzer
		)

		BeforeEach(func() {
			mock = &mockAnalyzer{}
			tsAnalyzer = treeSitterBasedAnalyzer{
				analyzer: mock,
				// resourceMapper is not used in analyze() method so we can leave it nil or mock if needed
				// wait, code checks: analysisResponse, err := a.analyzer.Analyze(ctx, input)
				// then iterates analysisResponse.MethodCalls
				// It does NOT use resourceMapper.
			}
		})

		It("should successfully analyze and return content", func(ctx context.Context) {
			// Setup mock response
			expectedMethodCalls := []models.MethodCall{
				{
					Name:     "testMethod",
					FilePath: "test.py",
					Line:     10,
				},
			}

			mock.analyzeFunc = func(ctx context.Context, input analyzercommon.AnalysisInput) (analyzercommon.AnalysisResult, error) {
				return analyzercommon.AnalysisResult{
					MethodCalls: expectedMethodCalls,
				}, nil
			}

			// Prepare request
			args := map[string]interface{}{
				"path": "/some/path",
			}
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: args,
				},
			}
			// Note: The code uses encodeutils.Convert(req.Params, &input).
			// req.Params.Arguments is map[string]interface{}.

			result, err := tsAnalyzer.analyze(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(result.Content).To(HaveLen(1))

			// Verify content
			text, ok := result.Content[0].(mcp.TextContent)
			Expect(ok).To(BeTrue())
			Expect(text.Text).To(ContainSubstring("testMethod"))
		})

		It("should return error when analyzer fails", func(ctx context.Context) {
			mock.analyzeFunc = func(ctx context.Context, input analyzercommon.AnalysisInput) (analyzercommon.AnalysisResult, error) {
				return analyzercommon.AnalysisResult{}, fmt.Errorf("analysis error")
			}

			args := map[string]interface{}{
				"path": "/some/path",
			}
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: args,
				},
			}

			result, err := tsAnalyzer.analyze(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("analysis error"))
			Expect(result).To(BeNil())
		})
	})
})
