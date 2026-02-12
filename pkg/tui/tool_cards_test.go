package tui

import (
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tool Cards", func() {
	Describe("summarizeToolArgs", func() {
		It("should extract file_name for read tools", func() {
			result := summarizeToolArgs("read_file", `{"file_name":"main.tf"}`)
			Expect(result).To(Equal("main.tf"))
		})

		It("should extract file_name for save tools", func() {
			result := summarizeToolArgs("save_file", `{"file_name":"vars.tf","content":"x"}`)
			Expect(result).To(Equal("vars.tf"))
		})

		It("should extract pattern for search tools", func() {
			result := summarizeToolArgs("search_files", `{"pattern":"resource"}`)
			Expect(result).To(Equal(`"resource"`))
		})

		It("should handle invalid JSON gracefully", func() {
			result := summarizeToolArgs("read_file", "not json")
			Expect(result).To(Equal(""))
		})
	})

	Describe("summarizeToolResult", func() {
		It("should count lines for read tool", func() {
			result := summarizeToolResult("read_file", "line1\nline2\nline3")
			Expect(result).To(Equal("3 lines read"))
		})

		It("should count lines for save tool", func() {
			result := summarizeToolResult("save_file", "wrote ok")
			Expect(result).To(Equal("1 line written"))
		})

		It("should handle empty response", func() {
			result := summarizeToolResult("read_file", "")
			Expect(result).To(Equal(""))
		})
	})

	Describe("extractDiffPreview", func() {
		It("should extract additions for save_file", func() {
			args := `{"file_name":"main.tf","content":"provider \"aws\" {\n  region = \"us-east-1\"\n}"}`
			result := extractDiffPreview("save_file", args)
			Expect(result).To(ContainSubstring("+ provider"))
			Expect(result).To(ContainSubstring("+ }"))
		})

		It("should extract diff for replace_content", func() {
			args := `{"file_name":"main.tf","old_content":"region = \"us-east-1\"","new_content":"region = \"us-west-2\""}`
			result := extractDiffPreview("replace_content", args)
			Expect(result).To(ContainSubstring("- region"))
			Expect(result).To(ContainSubstring("+ region"))
		})

		It("should return empty for read tools", func() {
			result := extractDiffPreview("read_file", `{"file_name":"main.tf"}`)
			Expect(result).To(Equal(""))
		})

		It("should truncate long diffs", func() {
			var lines string
			for i := 0; i < 20; i++ {
				lines += fmt.Sprintf("line %d\n", i)
			}
			argsMap := map[string]string{"file_name": "main.tf", "content": lines}
			argsBytes, _ := json.Marshal(argsMap)
			result := extractDiffPreview("save_file", string(argsBytes))
			Expect(result).To(ContainSubstring("more lines"))
		})

		It("should handle invalid JSON", func() {
			result := extractDiffPreview("save_file", "bad json")
			Expect(result).To(Equal(""))
		})
	})
})
