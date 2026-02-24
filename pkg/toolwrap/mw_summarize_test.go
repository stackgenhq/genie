package toolwrap_test

import (
	"context"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("AutoSummarizeMiddleware", func() {
	makeTC := func(name string) *toolwrap.ToolCallContext {
		return &toolwrap.ToolCallContext{ToolName: name, Args: []byte(`{}`)}
	}

	It("passes through responses below the threshold", func() {
		small := "hello world"
		mw := toolwrap.AutoSummarizeMiddleware(
			func(_ context.Context, _ string) (string, error) {
				Fail("summarizer should not be called for small responses")
				return "", nil
			},
			100, // low threshold for testing
		)
		handler := mw.Wrap(passthrough(small))
		result, err := handler(context.Background(), makeTC("read_file"))

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(small))
	})

	It("summarizes responses exceeding the threshold", func() {
		largeResponse := strings.Repeat("x", 600) // 600 chars, well above lowContentThreshold (500)
		var capturedContent string
		mw := toolwrap.AutoSummarizeMiddleware(
			func(_ context.Context, content string) (string, error) {
				capturedContent = content
				return "summarized output", nil
			},
			100,
		)
		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("http_request"))

		Expect(err).NotTo(HaveOccurred())
		Expect(capturedContent).To(Equal(largeResponse))
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(resultStr).To(ContainSubstring("Auto-summarized"))
		Expect(resultStr).To(ContainSubstring("summarized output"))
	})

	It("falls back to truncation when summarization fails", func() {
		largeResponse := strings.Repeat("z", 200)
		mw := toolwrap.AutoSummarizeMiddleware(
			func(_ context.Context, _ string) (string, error) {
				return "", errors.New("model error")
			},
			100,
		)
		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("web_search"))

		Expect(err).NotTo(HaveOccurred())
		// Should return a truncated string, not fail
		resultStr, ok := result.(string)
		Expect(ok).To(BeTrue())
		Expect(len(resultStr)).To(BeNumerically(">", 0))
	})

	It("is a no-op when summarize func is nil", func() {
		largeResponse := strings.Repeat("a", 200)
		mw := toolwrap.AutoSummarizeMiddleware(nil, 100)
		handler := mw.Wrap(passthrough(largeResponse))
		result, err := handler(context.Background(), makeTC("read_file"))

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(largeResponse))
	})

	It("does not summarize when the tool call itself errors", func() {
		mw := toolwrap.AutoSummarizeMiddleware(
			func(_ context.Context, _ string) (string, error) {
				Fail("summarizer should not be called on error results")
				return "", nil
			},
			100,
		)
		handler := mw.Wrap(func(_ context.Context, _ *toolwrap.ToolCallContext) (any, error) {
			return nil, errors.New("tool execution failed")
		})
		_, err := handler(context.Background(), makeTC("http_request"))
		Expect(err).To(HaveOccurred())
	})
})
