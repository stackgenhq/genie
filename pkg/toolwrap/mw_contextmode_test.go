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

	// --- Per-tool override tests ---

	It("applies per-tool override when tool name matches", func() {
		// Global threshold = 100, but run_shell override = 5000.
		// A 200-char response should be compressed globally but NOT for run_shell.
		response := strings.Repeat("data line\n\n", 50) // ~550 chars, many chunks
		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 100,
			MaxChunks: 3,
			ChunkSize: 50,
			PerTool: map[string]toolwrap.ContextModeToolOverride{
				"run_shell": {Threshold: 5000}, // Much higher — should skip compression.
			},
		})

		handler := mw.Wrap(passthrough(response))
		result, err := handler(context.Background(), makeTC("run_shell", `{"command":"kubectl get pods"}`))

		Expect(err).NotTo(HaveOccurred())
		// run_shell has threshold=5000, response is ~550 chars → should pass through.
		Expect(result).To(Equal(response))
	})

	It("falls back to global settings when tool name has no override", func() {
		response := strings.Repeat("data line\n\n", 50) // ~550 chars
		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 100,
			MaxChunks: 3,
			ChunkSize: 50,
			PerTool: map[string]toolwrap.ContextModeToolOverride{
				"run_shell": {Threshold: 5000},
			},
		})

		handler := mw.Wrap(passthrough(response))
		result, err := handler(context.Background(), makeTC("web_fetch", `{"url":"https://example.com"}`))

		Expect(err).NotTo(HaveOccurred())
		// web_fetch has no override → global threshold=100 applies → compressed.
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(resultStr).To(ContainSubstring("Context Mode: compressed"))
	})

	It("applies per-tool MaxChunks override", func() {
		// Build distinct paragraphs so chunking is deterministic.
		var parts []string
		for i := 0; i < 30; i++ {
			parts = append(parts, strings.Repeat("word"+strings.Repeat("x", i)+" ", 15))
		}
		largeResponse := strings.Join(parts, "\n\n")

		// Global: maxChunks=3, per-tool run_shell: maxChunks=20.
		globalMW := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 500,
			MaxChunks: 3,
			ChunkSize: 100,
		})
		perToolMW := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 500,
			MaxChunks: 3,
			ChunkSize: 100,
			PerTool: map[string]toolwrap.ContextModeToolOverride{
				"run_shell": {MaxChunks: 20},
			},
		})

		tc := makeTC("run_shell", `{"command":"ls -la"}`)

		globalHandler := globalMW.Wrap(passthrough(largeResponse))
		globalResult, err := globalHandler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())

		perToolHandler := perToolMW.Wrap(passthrough(largeResponse))
		perToolResult, err := perToolHandler(context.Background(), tc)
		Expect(err).NotTo(HaveOccurred())

		globalStr, _ := globalResult.(string)
		perToolStr, _ := perToolResult.(string)

		// Per-tool result should retain significantly more content.
		Expect(len(perToolStr)).To(BeNumerically(">", len(globalStr)))
		Expect(perToolStr).To(ContainSubstring("Context Mode: compressed"))
	})

	// --- Hex token extraction tests ---

	It("extracts hex-like pod IDs from tool arguments for scoring", func() {
		// Build a response where only a few chunks contain the pod ID "bc97d0".
		var parts []string
		for i := 0; i < 20; i++ {
			if i == 10 {
				parts = append(parts, "pod bc97d0 is in CrashLoopBackOff namespace production")
			} else {
				parts = append(parts, strings.Repeat("other unrelated pod data filler ", 10))
			}
		}
		largeResponse := strings.Join(parts, "\n\n")

		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold: 500,
			MaxChunks: 3,
			ChunkSize: 200,
		})

		handler := mw.Wrap(passthrough(largeResponse))
		// The command contains hex pod IDs — they should be extracted as query terms.
		result, err := handler(context.Background(), makeTC("run_shell",
			`{"command":"kubectl get pods -A | grep -E 'bc97d0|b5ae09'"}`))

		Expect(err).NotTo(HaveOccurred())
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(resultStr).To(ContainSubstring("bc97d0"))
	})

	// --- MinTermLen config tests ---

	It("respects MinTermLen configuration", func() {
		// Build response with 2-char tokens "ab".
		var parts []string
		for i := 0; i < 20; i++ {
			if i == 5 {
				parts = append(parts, strings.Repeat("ab data ab data ab ", 20))
			} else {
				parts = append(parts, strings.Repeat("unrelated filler content stuff ", 10))
			}
		}
		largeResponse := strings.Join(parts, "\n\n")

		// With MinTermLen=2, "ab" should be extracted as a query term.
		mw := toolwrap.ContextModeMiddleware(toolwrap.ContextModeConfig{
			Threshold:  500,
			MaxChunks:  3,
			ChunkSize:  200,
			MinTermLen: 2,
		})

		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("web_search", `{"query":"ab"}`))

		Expect(err).NotTo(HaveOccurred())
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		// The chunk containing "ab" should be ranked higher.
		Expect(resultStr).To(ContainSubstring("ab data"))
	})
})
