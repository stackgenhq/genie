package toolwrap_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("Justification", func() {
	Describe("extractJustification", func() {
		It("should extract the _justification key and strip it from args", func() {
			args := []byte(`{"query":"hello","_justification":"need this for search"}`)
			justification, stripped := toolwrap.ExtractJustificationForTest(args)
			Expect(justification).To(Equal("need this for search"))
			Expect(stripped).NotTo(ContainSubstring("_justification"))
			Expect(stripped).To(ContainSubstring("hello"))
		})

		It("should return empty string and unchanged args when no _justification present", func() {
			args := []byte(`{"query":"hello"}`)
			justification, stripped := toolwrap.ExtractJustificationForTest(args)
			Expect(justification).To(BeEmpty())
			Expect(stripped).To(Equal(args))
		})

		It("should handle empty args", func() {
			justification, stripped := toolwrap.ExtractJustificationForTest([]byte(`{}`))
			Expect(justification).To(BeEmpty())
			Expect(stripped).To(Equal([]byte(`{}`)))
		})

		It("should handle _justification as the only key", func() {
			args := []byte(`{"_justification":"solo"}`)
			justification, stripped := toolwrap.ExtractJustificationForTest(args)
			Expect(justification).To(Equal("solo"))
			Expect(stripped).To(MatchJSON(`{}`))
		})
	})
})
