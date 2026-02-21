package mcp_test

import (
	"github.com/appcd-dev/genie/pkg/mcp"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ClientTool Adapter", func() {
	var (
		mockTool mcplib.Tool
		adapter  *mcp.ClientTool
	)

	BeforeEach(func() {
		mockTool = mcplib.Tool{
			Name:        "test_tool",
			Description: "A test tool for testing",
			InputSchema: mcplib.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"arg1": map[string]interface{}{
						"type":        "string",
						"description": "First argument",
					},
					"arg2": map[string]interface{}{
						"type":        "integer",
						"description": "Second argument",
					},
				},
			},
		}
		// nil client is safe here because we only test metadata, not Call()
		adapter = mcp.NewClientTool(nil, mockTool, "testserver")
	})

	Describe("Name", func() {
		It("should return the namespaced tool name", func() {
			Expect(adapter.Name()).To(Equal("testserver_test_tool"))
		})
	})

	Describe("Description", func() {
		It("should return the MCP tool description", func() {
			Expect(adapter.Description()).To(Equal("A test tool for testing"))
		})
	})

	Describe("Declaration", func() {
		It("should return a valid tool.Declaration", func() {
			decl := adapter.Declaration()
			Expect(decl).NotTo(BeNil())
			Expect(decl.Name).To(Equal("testserver_test_tool"))
			Expect(decl.Description).To(Equal("A test tool for testing"))
		})

		It("should convert the MCP input schema", func() {
			decl := adapter.Declaration()
			Expect(decl.InputSchema).NotTo(BeNil())
			Expect(decl.InputSchema.Type).To(Equal("object"))
			Expect(decl.InputSchema.Properties).To(HaveKey("arg1"))
			Expect(decl.InputSchema.Properties).To(HaveKey("arg2"))
		})
	})

	Describe("tool.Tool interface compliance", func() {
		It("should satisfy tool.Tool via Declaration()", func() {
			// This test is compile-time verified by the type assertion.
			// The adapter must implement Declaration() *tool.Declaration.
			decl := adapter.Declaration()
			Expect(decl.Name).NotTo(BeEmpty())
		})
	})
})
