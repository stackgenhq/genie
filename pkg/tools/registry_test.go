package tools_test

import (
	"context"

	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/datetime"
	"github.com/stackgenhq/genie/pkg/tools/math"
)

func TestRegistry(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Registry Suite")
}

var _ = Describe("Registry", func() {
	Describe("GetToolDescriptions", func() {
		It("returns empty slice when registry has no tools", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx)
			descs := reg.GetToolDescriptions()
			Expect(descs).NotTo(BeNil())
			Expect(descs).To(BeEmpty())
		})

		It("returns name and description for each tool in name: description format", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, datetime.NewToolProvider())
			descs := reg.GetToolDescriptions()
			Expect(descs).To(HaveLen(1))
			Expect(descs[0]).To(HavePrefix("datetime:"))
			Expect(descs[0]).To(ContainSubstring("date"))
		})

		It("returns sorted by name: description string", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, datetime.NewToolProvider(), math.NewToolProvider())
			descs := reg.GetToolDescriptions()
			Expect(descs).To(HaveLen(3)) // datetime, math, calculator
			for i := 1; i < len(descs); i++ {
				Expect(descs[i] >= descs[i-1]).To(BeTrue(),
					"GetToolDescriptions should return lexicographically sorted slice: %q >= %q", descs[i], descs[i-1])
			}
		})

		It("includes every tool from the registry", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			names := reg.ToolNames()
			descs := reg.GetToolDescriptions()
			Expect(descs).To(HaveLen(len(names)))
			for _, name := range names {
				prefix := name + ": "
				Expect(descs).To(ContainElement(HavePrefix(prefix)),
					"GetToolDescriptions should include an entry for tool %q", name)
			}
		})
	})
})
