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
			justification, stripped, found := toolwrap.ExtractJustificationForTest(args)
			Expect(found).To(BeTrue())
			Expect(justification).To(Equal("need this for search"))
			Expect(stripped).NotTo(ContainSubstring("_justification"))
			Expect(stripped).To(ContainSubstring("hello"))
		})

		It("should return empty string and unchanged args when no _justification present", func() {
			args := []byte(`{"query":"hello"}`)
			justification, stripped, found := toolwrap.ExtractJustificationForTest(args)
			Expect(found).To(BeFalse())
			Expect(justification).To(BeEmpty())
			Expect(stripped).To(Equal(args))
		})

		It("should handle empty args", func() {
			justification, stripped, found := toolwrap.ExtractJustificationForTest([]byte(`{}`))
			Expect(found).To(BeFalse())
			Expect(justification).To(BeEmpty())
			Expect(stripped).To(Equal([]byte(`{}`)))
		})

		It("should handle _justification as the only key", func() {
			args := []byte(`{"_justification":"solo"}`)
			justification, stripped, found := toolwrap.ExtractJustificationForTest(args)
			Expect(found).To(BeTrue())
			Expect(justification).To(Equal("solo"))
			Expect(stripped).To(MatchJSON(`{}`))
		})

		It("should strip _justification even when value is an empty string", func() {
			args := []byte(`{"query":"hello","_justification":""}`)
			justification, stripped, found := toolwrap.ExtractJustificationForTest(args)
			Expect(found).To(BeTrue())
			Expect(justification).To(BeEmpty())
			Expect(stripped).NotTo(ContainSubstring("_justification"))
			Expect(stripped).To(ContainSubstring("hello"))
		})
	})
})
