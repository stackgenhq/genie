package doctool

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Document Tool (parse_document)", func() {
	var (
		d      *docTools
		tmpDir string
	)

	BeforeEach(func() {
		d = newDocTools()
		var err error
		tmpDir, err = os.MkdirTemp("", "doctool-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("text files", func() {
		It("reads plain text files", func() {
			path := filepath.Join(tmpDir, "test.txt")
			Expect(os.WriteFile(path, []byte("Hello, World!\nLine 2"), 0644)).To(Succeed())

			resp, err := d.parse(context.Background(), docRequest{FilePath: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Format).To(Equal("text"))
			Expect(resp.Content).To(Equal("Hello, World!\nLine 2"))
			Expect(resp.LineCount).To(Equal(2))
			Expect(resp.CharCount).To(Equal(20))
		})

		It("reads markdown files", func() {
			path := filepath.Join(tmpDir, "readme.md")
			Expect(os.WriteFile(path, []byte("# Title\n\nContent"), 0644)).To(Succeed())

			resp, err := d.parse(context.Background(), docRequest{FilePath: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Format).To(Equal("text"))
			Expect(resp.Content).To(ContainSubstring("# Title"))
		})
	})

	Describe("CSV files", func() {
		It("parses CSV into markdown table format", func() {
			path := filepath.Join(tmpDir, "data.csv")
			csvData := "Name,Age,City\nAlice,30,NYC\nBob,25,LA\n"
			Expect(os.WriteFile(path, []byte(csvData), 0644)).To(Succeed())

			resp, err := d.parse(context.Background(), docRequest{FilePath: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Format).To(Equal("csv"))
			Expect(resp.Content).To(ContainSubstring("| Name | Age | City |"))
			Expect(resp.Content).To(ContainSubstring("| Alice | 30 | NYC |"))
			Expect(resp.LineCount).To(Equal(3))
		})

		It("handles empty CSV", func() {
			path := filepath.Join(tmpDir, "empty.csv")
			Expect(os.WriteFile(path, []byte(""), 0644)).To(Succeed())

			resp, err := d.parse(context.Background(), docRequest{FilePath: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Content).To(ContainSubstring("empty"))
		})
	})

	Describe("DOCX files", func() {
		It("extracts text from DOCX", func() {
			path := filepath.Join(tmpDir, "test.docx")
			createTestDOCX(path, "Hello from DOCX")

			resp, err := d.parse(context.Background(), docRequest{FilePath: path})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Format).To(Equal("docx"))
			Expect(resp.Content).To(ContainSubstring("Hello from DOCX"))
		})
	})

	Describe("format override", func() {
		It("allows manual format specification", func() {
			path := filepath.Join(tmpDir, "data.txt")
			csvData := "a,b,c\n1,2,3\n"
			Expect(os.WriteFile(path, []byte(csvData), 0644)).To(Succeed())

			resp, err := d.parse(context.Background(), docRequest{FilePath: path, Format: "csv"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Format).To(Equal("csv"))
			Expect(resp.Content).To(ContainSubstring("| a | b | c |"))
		})
	})

	Describe("error cases", func() {
		It("returns error for empty path", func() {
			_, err := d.parse(context.Background(), docRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("file_path is required"))
		})

		It("returns error for non-existent file", func() {
			_, err := d.parse(context.Background(), docRequest{FilePath: "/nonexistent/file.txt"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot access"))
		})

		It("returns error for directory path", func() {
			_, err := d.parse(context.Background(), docRequest{FilePath: tmpDir})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("directory"))
		})
	})
})

var _ = Describe("detectFormat", func() {
	var d *docTools

	BeforeEach(func() {
		d = newDocTools()
	})

	DescribeTable("detects format from extension",
		func(filename, expected string) {
			Expect(d.detectFormat(filename)).To(Equal(expected))
		},
		Entry("PDF", "report.pdf", "pdf"),
		Entry("DOCX", "letter.docx", "docx"),
		Entry("CSV", "data.csv", "csv"),
		Entry("text", "readme.txt", "text"),
		Entry("markdown", "guide.md", "text"),
		Entry("JSON", "config.json", "text"),
		Entry("Go source", "main.go", "text"),
		Entry("unknown", "file.xyz", "text"),
	)
})

var _ = Describe("Document ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("parse_document"))
	})
})

// createTestDOCX creates a minimal valid DOCX file with the given text content.
func createTestDOCX(path, content string) {
	f, err := os.Create(path)
	Expect(err).NotTo(HaveOccurred())
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	// Write word/document.xml with the content.
	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>` + content + `</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`

	fw, err := w.Create("word/document.xml")
	Expect(err).NotTo(HaveOccurred())
	_, err = fw.Write([]byte(docXML))
	Expect(err).NotTo(HaveOccurred())

	// Write [Content_Types].xml (required for valid DOCX).
	ct, err := w.Create("[Content_Types].xml")
	Expect(err).NotTo(HaveOccurred())
	_, err = ct.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="xml" ContentType="application/xml"/>
</Types>`))
	Expect(err).NotTo(HaveOccurred())
}
