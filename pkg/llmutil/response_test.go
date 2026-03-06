package llmutil_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/llmutil"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("ExtractChoiceContent", func() {
	It("returns empty string for nil choices", func() {
		Expect(llmutil.ExtractChoiceContent(nil)).To(BeEmpty())
	})

	It("returns empty string for empty choices", func() {
		Expect(llmutil.ExtractChoiceContent([]model.Choice{})).To(BeEmpty())
	})

	It("returns Message.Content from the last choice with content", func() {
		choices := []model.Choice{
			{Message: model.Message{Content: "first"}},
			{Message: model.Message{Content: ""}},
			{Message: model.Message{Content: "last"}},
		}
		Expect(llmutil.ExtractChoiceContent(choices)).To(Equal("last"))
	})

	It("falls back to Delta.Content when Message.Content is empty", func() {
		choices := []model.Choice{
			{Delta: model.Message{Content: "delta-first"}},
			{Delta: model.Message{Content: "delta-last"}},
		}
		Expect(llmutil.ExtractChoiceContent(choices)).To(Equal("delta-last"))
	})

	It("prefers Message.Content over Delta.Content in the same choice", func() {
		choices := []model.Choice{
			{
				Message: model.Message{Content: "msg"},
				Delta:   model.Message{Content: "delta"},
			},
		}
		Expect(llmutil.ExtractChoiceContent(choices)).To(Equal("msg"))
	})

	It("returns empty string when all choices have empty content", func() {
		choices := []model.Choice{
			{Message: model.Message{Content: ""}, Delta: model.Message{Content: ""}},
			{Message: model.Message{Content: ""}, Delta: model.Message{Content: ""}},
		}
		Expect(llmutil.ExtractChoiceContent(choices)).To(BeEmpty())
	})

	It("scans from the end and returns the last non-empty content", func() {
		choices := []model.Choice{
			{Message: model.Message{Content: "early"}},
			{Message: model.Message{Content: ""}},
			{Message: model.Message{Content: ""}},
			{Message: model.Message{Content: "final"}},
		}
		Expect(llmutil.ExtractChoiceContent(choices)).To(Equal("final"))
	})
})
