package reactree_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/reactree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// MockTool implements tool.Tool interface for testing
type MockTool struct {
	name        string
	description string
}

func (m *MockTool) Name() string {
	return m.name
}

func (m *MockTool) Description() string {
	return m.description
}

func (m *MockTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        m.name,
		Description: m.description,
	}
}

func (m *MockTool) Run(ctx context.Context, input []byte) ([]byte, error) {
	return nil, nil
}

var _ = Describe("ToolRegistry", func() {
	Describe("String", func() {
		It("should return a formatted string with tool names and descriptions", func() {
			registry := make(reactree.ToolRegistry)
			registry["tool1"] = &MockTool{name: "tool1", description: "Description for tool 1"}
			registry["tool2"] = &MockTool{name: "tool2", description: "Description for tool 2"}

			output := registry.String()

			Expect(output).To(ContainSubstring("tool1"))
			Expect(output).To(ContainSubstring("Description for tool 1"))
			Expect(output).To(ContainSubstring("tool2"))
			Expect(output).To(ContainSubstring("Description for tool 2"))

			// Verify format: \n<name>\t<description>
			Expect(output).To(ContainSubstring("\ntool1\tDescription for tool 1"))
			Expect(output).To(ContainSubstring("\ntool2\tDescription for tool 2"))
		})

		It("should handle empty registry", func() {
			registry := make(reactree.ToolRegistry)
			output := registry.String()
			Expect(output).To(BeEmpty())
		})
	})

	Describe("Exclude", func() {
		It("should exclude specified tools", func() {
			registry := make(reactree.ToolRegistry)
			registry["tool1"] = &MockTool{name: "tool1"}
			registry["tool2"] = &MockTool{name: "tool2"}
			registry["tool3"] = &MockTool{name: "tool3"}

			newRegistry := registry.Exclude([]string{"tool1", "tool3"})

			Expect(newRegistry).To(HaveLen(1))
			Expect(newRegistry).To(HaveKey("tool2"))
			Expect(newRegistry).NotTo(HaveKey("tool1"))
			Expect(newRegistry).NotTo(HaveKey("tool3"))
		})

		It("should not modify the original registry", func() {
			registry := make(reactree.ToolRegistry)
			registry["tool1"] = &MockTool{name: "tool1"}

			newRegistry := registry.Exclude([]string{"tool1"})

			Expect(newRegistry).To(BeEmpty())
			Expect(registry).To(HaveLen(1))
			Expect(registry).To(HaveKey("tool1"))
		})

		It("should handle excluding non-existent tools", func() {
			registry := make(reactree.ToolRegistry)
			registry["tool1"] = &MockTool{name: "tool1"}

			newRegistry := registry.Exclude([]string{"non-existent"})

			Expect(newRegistry).To(HaveLen(1))
			Expect(newRegistry).To(HaveKey("tool1"))
		})

		It("should handle empty exclusion list", func() {
			registry := make(reactree.ToolRegistry)
			registry["tool1"] = &MockTool{name: "tool1"}

			newRegistry := registry.Exclude([]string{})

			Expect(newRegistry).To(HaveLen(1))
			Expect(newRegistry).To(HaveKey("tool1"))
		})
	})
})
