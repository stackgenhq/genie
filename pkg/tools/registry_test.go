package tools_test

import (
	"context"

	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/datetime"
	"github.com/stackgenhq/genie/pkg/tools/math"
	"trpc.group/trpc-go/trpc-agent-go/tool"
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

	Describe("GetTool and GetTools", func() {
		It("GetTool returns the specific tool", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, math.NewToolProvider())
			t, err := reg.GetTool("calculator")
			Expect(err).NotTo(HaveOccurred())
			Expect(t).NotTo(BeNil())
			Expect(t.Declaration().Name).To(Equal("calculator"))

			_, err = reg.GetTool("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("tool not found"))
		})

		It("GetTools and AllTools return the full slice", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, math.NewToolProvider())

			toolsList := reg.GetTools()
			Expect(toolsList).NotTo(BeEmpty())

			all := reg.AllTools()
			Expect(all).To(HaveLen(len(toolsList)))
		})
	})

	Describe("FilterDenied", func() {
		It("excludes denied tools", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, math.NewToolProvider())

			// math has calculator. What if we deny it?
			cfg := hitl.Config{
				DeniedTools: []string{"calculator"},
			}
			filtered := reg.FilterDenied(ctx, cfg)

			// Verify filtered registry lacks calculator
			Expect(filtered.ToolNames()).NotTo(ContainElement("calculator"))
			// Verify original registry still has it
			Expect(reg.ToolNames()).To(ContainElement("calculator"))
		})
	})

	Describe("Include and Exclude", func() {
		It("Exclude strips out tools", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())

			Expect(reg.ToolNames()).To(ContainElements("calculator", "datetime"))

			filtered := reg.Exclude("calculator")
			Expect(filtered.ToolNames()).NotTo(ContainElement("calculator"))
			Expect(filtered.ToolNames()).To(ContainElement("datetime"))
		})

		It("Include limits to specified tools", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, math.NewToolProvider(), datetime.NewToolProvider())

			filtered := reg.Include("calculator")
			Expect(filtered.ToolNames()).To(ContainElement("calculator"))
			Expect(filtered.ToolNames()).NotTo(ContainElement("datetime"))
		})
	})

	Describe("CloneWithEphemeralProviders", func() {
		It("clones CloneableToolProvider correctly", func() {
			ctx := context.Background()
			reg := tools.NewRegistry(ctx, &mockCloneableProvider{})
			cloned := reg.CloneWithEphemeralProviders()

			// Verify the original has no tools (dummy) but it processed correctly without panic
			Expect(cloned).NotTo(BeNil())
		})
	})
})

type mockCloneableProvider struct{}

func (m *mockCloneableProvider) GetTools() []tool.Tool      { return nil }
func (m *mockCloneableProvider) Clone() tools.ToolProviders { return &mockCloneableProvider{} }
