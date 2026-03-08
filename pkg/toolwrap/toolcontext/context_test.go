package toolcontext_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	toolcontext "github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
)

func TestToolContext(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "ToolContext Suite")
}

var _ = Describe("ToolContext", func() {

	Describe("WithJustification / GetJustification", func() {
		It("should round-trip a justification through context", func() {
			ctx := toolcontext.WithJustification(context.Background(), "important reason")
			Expect(toolcontext.GetJustification(ctx)).To(Equal("important reason"))
		})

		It("should return empty string from a bare context", func() {
			Expect(toolcontext.GetJustification(context.Background())).To(BeEmpty())
		})

		It("should return the most recently set justification", func() {
			ctx := toolcontext.WithJustification(context.Background(), "first")
			ctx = toolcontext.WithJustification(ctx, "second")
			Expect(toolcontext.GetJustification(ctx)).To(Equal("second"))
		})

		It("should handle empty string justification", func() {
			ctx := toolcontext.WithJustification(context.Background(), "")
			Expect(toolcontext.GetJustification(ctx)).To(BeEmpty())
		})
	})

	Describe("WithSkipSummarizeSetter / GetSkipSummarizeSetter", func() {
		It("should round-trip a setter function through context", func() {
			called := false
			setter := func() { called = true }
			ctx := toolcontext.WithSkipSummarizeSetter(context.Background(), setter)
			retrieved := toolcontext.GetSkipSummarizeSetter(ctx)
			Expect(retrieved).NotTo(BeNil())
			retrieved()
			Expect(called).To(BeTrue())
		})

		It("should return a no-op function from a bare context", func() {
			setter := toolcontext.GetSkipSummarizeSetter(context.Background())
			Expect(setter).NotTo(BeNil())
			// Should not panic when called
			setter()
		})

		It("should return the most recently set setter", func() {
			firstCalled := false
			secondCalled := false
			ctx := toolcontext.WithSkipSummarizeSetter(context.Background(), func() { firstCalled = true })
			ctx = toolcontext.WithSkipSummarizeSetter(ctx, func() { secondCalled = true })
			toolcontext.GetSkipSummarizeSetter(ctx)()
			Expect(firstCalled).To(BeFalse())
			Expect(secondCalled).To(BeTrue())
		})
	})
})
