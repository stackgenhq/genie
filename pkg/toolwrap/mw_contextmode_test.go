package toolwrap_test

import (
	"context"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("ContextModeMiddleware", func() {
	makeTC := func(name string, args string) *toolwrap.ToolCallContext {
		return &toolwrap.ToolCallContext{ToolName: name, Args: []byte(args)}
	}

	It("passes through responses below the threshold", func() {
		small := "hello world"
		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 100,
			MaxChunks: 5,
		})
		handler := mw.Wrap(passthrough(small))
		result, err := handler(context.Background(), makeTC("read_file", `{"path":"/tmp/foo.go"}`))

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(small))
	})

	It("compresses large responses above the threshold", func() {
		// Build a large response with distinct paragraphs containing different terms.
		var parts []string
		for i := 0; i < 30; i++ {
			if i%3 == 0 {
				parts = append(parts, strings.Repeat("authentication security token ", 20))
			} else {
				parts = append(parts, strings.Repeat("unrelated filler content padding ", 20))
			}
		}
		largeResponse := strings.Join(parts, "\n\n")

		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 500,
			MaxChunks: 5,
			ChunkSize: 200,
		})

		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("http_request", `{"url":"https://example.com/auth","method":"GET"}`))

		Expect(err).NotTo(HaveOccurred())
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(resultStr).To(ContainSubstring("Context Mode: compressed"))
		Expect(len(resultStr)).To(BeNumerically("<", len(largeResponse)))
	})

	It("preserves boundary chunks when no query terms are available", func() {
		var parts []string
		for i := 0; i < 20; i++ {
			parts = append(parts, strings.Repeat("x", 200))
		}
		largeResponse := strings.Join(parts, "\n\n")

		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 500,
			MaxChunks: 4,
			ChunkSize: 200,
		})

		// Empty args — no query terms to extract.
		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("web_search", `{}`))

		Expect(err).NotTo(HaveOccurred())
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(resultStr).To(ContainSubstring("boundary selection"))
		Expect(len(resultStr)).To(BeNumerically("<", len(largeResponse)))
	})

	It("is a no-op when chunk count is below maxChunks", func() {
		// Response that splits into fewer chunks than maxChunks.
		response := "paragraph one\n\nparagraph two\n\nparagraph three"
		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 10, // Low threshold so it activates.
			MaxChunks: 10, // More than chunk count.
			ChunkSize: 200,
		})

		handler := mw.Wrap(passthrough(response))
		result, err := handler(context.Background(), makeTC("read_file", `{"path":"/tmp/foo"}`))

		Expect(err).NotTo(HaveOccurred())
		// Should pass through because chunk count <= maxChunks.
		Expect(result).To(Equal(response))
	})

	It("does not process when the tool call itself errors", func() {
		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 10,
			MaxChunks: 5,
		})
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return nil, errors.New("tool execution failed")
		})
		_, err := handler(context.Background(), makeTC("http_request", `{"url":"https://example.com"}`))
		Expect(err).To(HaveOccurred())
	})

	It("ranks chunks containing query terms higher", func() {
		// Build response where only some chunks contain the query term "kubernetes".
		relevantChunk := "kubernetes cluster deployment pods services namespace"
		irrelevantChunk := strings.Repeat("lorem ipsum dolor sit amet ", 10)

		var parts []string
		for i := 0; i < 20; i++ {
			if i == 5 || i == 15 {
				parts = append(parts, strings.Repeat(relevantChunk+" ", 5))
			} else {
				parts = append(parts, irrelevantChunk)
			}
		}
		largeResponse := strings.Join(parts, "\n\n")

		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 500,
			MaxChunks: 3,
			ChunkSize: 200,
		})

		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("web_search", `{"query":"kubernetes deployment"}`))

		Expect(err).NotTo(HaveOccurred())
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(resultStr).To(ContainSubstring("kubernetes"))
		Expect(resultStr).To(ContainSubstring("Context Mode: compressed"))
	})
})
