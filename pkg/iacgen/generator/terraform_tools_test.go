package generator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TerraformTools", func() {
	Describe("NewTerraformTools", func() {
		It("should initialize a new instance", func() {
			tools := NewTerraformTools(5)
			Expect(tools).ToNot(BeNil())
			Expect(tools.registryTools).ToNot(BeNil())
		})
	})

	Describe("GetTools", func() {
		It("should return a list of tools including search_modules", func() {
			tfTools := NewTerraformTools(5)
			toolsList := tfTools.GetTools()

			Expect(toolsList).ToNot(BeEmpty())

			// Check for specific tool names
			toolNames := make([]string, 0, len(toolsList))
			for _, t := range toolsList {
				toolNames = append(toolNames, t.Declaration().Name)
			}

			Expect(toolNames).To(ContainElements("search_modules", "get_module_details"))
		})
	})
})
